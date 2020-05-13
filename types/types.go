package types

// UserObject defines attributes retrieved by client for a give user
// Relation can be one of "friends", "followers", "retweeter", or list name
// Subject is the handle for which the record relation holds e.g. membership of list
type UserObject struct {
	ID              uint64 `json:"id"`
	ScreenName      string `json:"screen_name"`
	Name            string `json:"name"`
	FriendsCount    int    `json:"friends_count"`
	FollowersCount  int    `json:"followers_count"`
	ListedCount     int    `json:"listed_count"`
	StatusesCount   int    `json:"statuses_count"`
	CreatedAt       string `json:"created_at"`
	URL             string `json:"url"`
	ProfileImageURL string `json:"profile_image_url"`
	Location        string `json:"location"`
	Description     string `json:"description"`
	Relation        string
	Subject         string
}
