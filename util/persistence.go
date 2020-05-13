package util

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"nucoll/types"

	"github.com/yaricom/goGraphML/graphml"
)

const (
	// FdatDir friends file directory
	FdatDir string = "fdat"
	// ImgDir avatar images directory
	ImgDir string = "img"
	// DatExt Init files extension
	DatExt string = ".dat"
	// FdatExt Fetch files extension
	FdatExt string = ".f"
	// QueryExt custom list or query result extension
	QueryExt string = ".qry" // previously .twt
	// GmlExt network graph extension
	GmlExt string = ".gml"
)

// QueryReader extracts twitter handles from query file
func QueryReader(handle string, firstHandleOnly bool) ([]string, error) {
	var handles []string

	twtFile, err := os.Open(handle + QueryExt)
	if err != nil {
		return nil, err
	}
	defer twtFile.Close()

	re := regexp.MustCompile("@([\\w]+)")
	scanner := bufio.NewScanner(twtFile)
	for scanner.Scan() {
		if firstHandleOnly {
			h := re.FindStringSubmatch(scanner.Text())
			if len(h) > 1 && !Exists(h[1], handles) {
				handles = append(handles, h[1])
			}
		} else {
			for _, h := range re.FindAllStringSubmatch(scanner.Text(), -1) {
				if len(h) > 1 && !Exists(h[1], handles) {
					handles = append(handles, h[1])
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return handles, nil
}

// CSVReader dynamic data loader
// Expect first row to contain struct field names preceded by comment hash '#'
func CSVReader(handle string, ext string, data interface{}) error {
	csvFile, err := os.Open(handle + ext)
	if err != nil {
		return err
	}
	defer csvFile.Close()
	reader := csv.NewReader(csvFile)
	reader.Comment = '#'
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	appendFlag := false
	out := reflect.ValueOf(data).Elem()
	if out.Len() > 0 {
		appendFlag = true
	}
	if !appendFlag {
		out.Set(reflect.MakeSlice(out.Type(), len(records), len(records)))
	}
	for j, r := range records {
		val := reflect.New(out.Type().Elem()).Elem()
		if val.NumField() != len(r) {
			continue
		}
		for i := 0; i < val.NumField(); i++ {
			fv := val.Field(i)
			switch fv.Kind() {
			case reflect.String:
				fv.SetString(r[i])
			case reflect.Uint64:
				ri, err := strconv.ParseUint(r[i], 10, 64)
				if err != nil {
					return err
				}
				fv.SetUint(ri)
			case reflect.Int:
				ri, err := strconv.ParseInt(r[i], 10, 32)
				if err != nil {
					return err
				}
				fv.SetInt(ri)
			}
		}
		if appendFlag {
			out.Set(reflect.Append(out, val))
		} else {
			out.Index(j).Set(val)
		}
	}

	return nil
}

// CSVWriter dynamic data writer
func CSVWriter(handle string, ext string, appendFlag bool, data interface{}) (string, error) {
	items := reflect.ValueOf(data)
	if items.Kind() != reflect.Slice || items.Len() == 0 {
		return "", nil
	}

	perm := os.O_WRONLY
	if appendFlag {
		perm = os.O_APPEND | perm
	} else {
		perm = os.O_CREATE | os.O_TRUNC | perm
	}
	filename := handle + ext
	csvFile, err := os.OpenFile(filename, perm, 0644)
	if err != nil {
		return "", err
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)

	if !appendFlag {
		t := items.Index(0)
		header := make([]string, t.NumField())
		for i := range header {
			header[i] = t.Type().Field(i).Name
			if i == 0 {
				header[i] = "#" + header[i]
			}
		}
		if err := writer.Write(header); err != nil {
			return "", nil
		}
		writer.Flush()
	}

	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)
		t := reflect.Indirect(item)
		cols := make([]string, t.NumField())
		for i := range cols {
			switch t.Field(i).Kind() {
			case reflect.Struct:
				if t.Field(i).Type().Field(0).Name == "ScreenName" {
					cols[i] = fmt.Sprintf("@%v", t.Field(i).Field(0).Interface())
				}
			case reflect.String:
				cols[i] = strings.Replace(fmt.Sprintf("%v", t.Field(i).Interface()), "\n", " ", -1)
			case reflect.Uint64:
				fallthrough
			case reflect.Int:
				cols[i] = fmt.Sprintf("%v", t.Field(i).Interface())
			}
		}
		if err := writer.Write(cols); err != nil {
			return "", err
		}
		writer.Flush()
	}

	if err := writer.Error(); err != nil {
		return "", err
	}

	return filename, nil
}

// FdatExists if friends file found
func FdatExists(handle string) bool {
	if _, err := os.Stat(FdatDir + "/" + handle + FdatExt); os.IsNotExist(err) {
		return false
	}
	return true
}

// FdatWriter spills list of friends to disk
func FdatWriter(handle string, ids []string) (string, error) {
	if _, err := os.Stat(FdatDir); os.IsNotExist(err) {
		if err := os.MkdirAll(FdatDir, 0755); err != nil {
			return "", err
		}
	}

	const perm = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	filename := FdatDir + "/" + handle + FdatExt
	fdatFile, err := os.OpenFile(filename, perm, 0644)
	if err != nil {
		return "", err
	}
	defer fdatFile.Close()
	for _, id := range ids {
		fdatFile.WriteString(id + "\n")
	}
	return filename, nil
}

// DownloadImage save avatar for user id
func DownloadImage(id uint64, url string) (string, error) {
	if _, err := os.Stat(ImgDir); os.IsNotExist(err) {
		if err := os.MkdirAll(ImgDir, 0755); err != nil {
			return "", err
		}
	}

	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if _, err := os.Stat(ImgDir); os.IsNotExist(err) {
		if err := os.Mkdir(ImgDir, 0777); err != nil {
			return "", err
		}
	}
	filename := filepath.Join(ImgDir, fmt.Sprintf("%d", id))
	switch res.Header.Get("Content-Type") {
	case "image/gif":
		filename += ".gif"
	case "image/pjpeg":
		fallthrough
	case "image/jpeg":
		filename += ".jpg"
	case "image/png":
		filename += ".png"
	}
	image, err := os.Create(filename)
	defer image.Close()
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(image, res.Body); err != nil {
		return "", err
	}

	return filename, nil
}

// GraphMLWriter save with GraphML
func GraphMLWriter(handles []string, data []types.UserObject, includeMissingIDs bool) (string, error) {
	gml := graphml.NewGraphML("Twitter network")

	// not sure what this is for
	graphAttributes := make(map[string]interface{})
	graph, err := gml.AddGraph("Twitter network", graphml.EdgeDirectionDirected, graphAttributes)
	if err != nil {
		log.Fatal(err)
	}

	gml.RegisterKey(graphml.KeyForNode, "TwitterID", "TwitterID", reflect.Int64, nil)
	gml.RegisterKey(graphml.KeyForNode, "ScreenName", "ScreenName", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "Name", "Name", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "FriendsCount", "FriendsCount", reflect.Int, nil)
	gml.RegisterKey(graphml.KeyForNode, "FollowersCount", "FollowersCount", reflect.Int, nil)
	gml.RegisterKey(graphml.KeyForNode, "ListedCount", "ListedCount", reflect.Int, nil)
	gml.RegisterKey(graphml.KeyForNode, "StatusesCount", "StatusesCount", reflect.Int, nil)
	gml.RegisterKey(graphml.KeyForNode, "CreatedAt", "CreatedAt", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "URL", "URL", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "ProfileImageURL", "ProfileImageURL", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "Location", "Location", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "Description", "Description", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "Relation", "Relation", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "Subject", "Subject", reflect.String, nil)
	gml.RegisterKey(graphml.KeyForNode, "Processed", "Processed", reflect.Bool, nil)

	filename := strings.Join(handles, "_") + ".graphml"

	friendMap := make(map[string]string)
	handleMap := make(map[string]string)
	nodeMap := make(map[string]*graphml.Node)

	for _, userObject := range data {
		idStr := strconv.FormatUint(userObject.ID, 10)
		processed := FdatExists(idStr)
		if !includeMissingIDs && !processed && userObject.Subject != "" {
			continue
		}

		friendMap[idStr] = userObject.Subject
		if userObject.Subject == "" {
			handleMap[idStr] = userObject.ScreenName
		}

		// this is technically dangerous since userObject.ID is uint64
		// however in practice the Twitter IDs (seemingly) won't overflow this
		// just need an Int64 to satisfy the graphml library
		// should probably just modify every uint64 in this library into an int64
		numericID, err := strconv.ParseInt(idStr, 10, 64)

		attributes := make(map[string]interface{})
		attributes["TwitterID"] = numericID
		attributes["ScreenName"] = userObject.ScreenName
		attributes["Name"] = userObject.Name
		attributes["FriendsCount"] = userObject.FriendsCount
		attributes["FollowersCount"] = userObject.FollowersCount
		attributes["ListedCount"] = userObject.ListedCount
		attributes["StatusesCount"] = userObject.StatusesCount
		attributes["CreatedAt"] = userObject.CreatedAt
		attributes["URL"] = userObject.URL
		attributes["ProfileImageURL"] = userObject.ProfileImageURL
		attributes["Location"] = userObject.Location
		attributes["Description"] = userObject.Description
		attributes["Relation"] = userObject.Relation
		attributes["Subject"] = userObject.Subject
		attributes["Processed"] = processed

		node, err := graph.AddNode(attributes, "")
		if err != nil {
			log.Fatal(err)
		}
		nodeMap[idStr] = node

	}
	for from, subject := range friendMap {
		switch {
		// handle ego case
		case subject == "":
			for to, handle := range friendMap {
				if handle == handleMap[from] {

					fromNode := nodeMap[from]
					toNode := nodeMap[to]

					// not sure what this is for
					edgeAttributes := make(map[string]interface{})

					graph.AddEdge(fromNode, toNode, edgeAttributes, graphml.EdgeDirectionDefault, "")
				}
			}
			continue
		default:
			fdatFile, err := os.Open(FdatDir + "/" + from + FdatExt)
			if err != nil {
				continue
			}
			defer fdatFile.Close()
			scanner := bufio.NewScanner(fdatFile)
			for scanner.Scan() {
				to := scanner.Text()
				if friendMap[to] != "" || handleMap[to] != "" {

					fromNode := nodeMap[from]
					toNode := nodeMap[to]

					// not sure what this is for
					edgeAttributes := make(map[string]interface{})

					graph.AddEdge(fromNode, toNode, edgeAttributes, graphml.EdgeDirectionDefault, "")
				}
			}
			if err := scanner.Err(); err != nil {
				return "", err
			}
		}
	}

	myFile, err := os.Create(filename)
	defer myFile.Close()
	err = gml.Encode(myFile, false)

	return filename, nil

}

// GMLWriter generates GML file for given array of handles using cols as node properties and sets label to given attribute
// spec from http://www.fim.uni-passau.de/fileadmin/files/lehrstuhl/brandenburg/projekte/gml/gml-technical-report.pdf
func GMLWriter(handles []string, data interface{}, includeMissingIDs bool, cols []string, label string) (string, error) {
	const perm = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	replacer := strings.NewReplacer("\"", "'")

	items := reflect.ValueOf(data)
	if items.Kind() != reflect.Slice || items.Len() == 0 {
		return "", nil
	}

	filename := strings.Join(handles, "_") + GmlExt
	gmlFile, err := os.OpenFile(filename, perm, 0644)
	if err != nil {
		return "", err
	}
	defer gmlFile.Close()

	friendMap := make(map[string]string)
	handleMap := make(map[string]string)
	gmlFile.WriteString("graph [\n  directed 1\n")
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)
		t := reflect.Indirect(item)
		subject := fmt.Sprintf("%v", t.FieldByName("Subject").Interface())
		handle := fmt.Sprintf("%v", t.FieldByName("ScreenName").Interface())
		processed := FdatExists(fmt.Sprintf("%v", t.FieldByName("ID").Interface()))
		if !includeMissingIDs && !processed && subject != "" {
			continue
		}
		gmlFile.WriteString("  node [\n")
		for _, c := range cols {
			v := fmt.Sprintf("%v", t.FieldByName(c).Interface())
			digitsOnly := false
			switch {
			case c == "ID":
				friendMap[v] = subject
				if subject == "" {
					handleMap[v] = handle
				}
				digitsOnly = true
			case c == label:
				c = "Label"
			case c == "FriendsCount":
				digitsOnly = true
			case c == "FollowersCount":
				digitsOnly = true
			case c == "ListedCount":
				digitsOnly = true
			case c == "StatusesCount":
				digitsOnly = true
			}
			if digitsOnly {
				gmlFile.WriteString(fmt.Sprintf("    %s %v\n", c, v))
			} else {
				// gml can't handle " so just replace with ' instead of escaping or replacing with space
				escapedString := replacer.Replace(v)

				// remove pesky control characters etc which interfere with xml
				escapedString = strings.TrimFunc(escapedString, func(r rune) bool {
					return !unicode.IsGraphic(r)
				})
				gmlFile.WriteString(fmt.Sprintf("    %s \"%v\"\n", c, escapedString))
			}
		}
		gmlFile.WriteString(fmt.Sprintf("    Processed \"%t\"\n", processed))
		gmlFile.WriteString("  ]\n")
	}
	for from, subject := range friendMap {
		switch {
		// handle ego case
		case subject == "":
			for to, handle := range friendMap {
				if handle == handleMap[from] {
					gmlFile.WriteString(fmt.Sprintf("  edge [\n    source %s\n    target %s\n  ]\n", from, to))
				}
			}
			continue
		default:
			fdatFile, err := os.Open(FdatDir + "/" + from + FdatExt)
			if err != nil {
				continue
			}
			defer fdatFile.Close()
			scanner := bufio.NewScanner(fdatFile)
			for scanner.Scan() {
				to := scanner.Text()
				if friendMap[to] != "" || handleMap[to] != "" {
					gmlFile.WriteString(fmt.Sprintf("  edge [\n    source %s\n    target %s\n  ]\n", from, to))
				}
			}
			if err := scanner.Err(); err != nil {
				return "", err
			}
		}
	}
	gmlFile.WriteString("]\n")

	return filename, nil
}
