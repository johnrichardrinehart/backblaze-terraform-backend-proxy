package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

const authURL = "https://api.backblazeb2.com/b2api/v2/b2_authorize_account"

type B2 struct {
	Token   string // auth token for API calls - valid for 24h
	Path    string // path in B2 to the state file
	BaseURL string // returned by an API call
}

type allowed struct {
	Capabilities []string
	BucketID     string `json:",omitempty"`
	BucketName   string `json:",omitempty"`
	NamePrefix   string `json:",omitempty"`
}

type responseAuthorizeAccount struct {
	AccountID               string
	AuthorizationToken      string
	Allowed                 allowed
	ApiUrl                  string
	DownloadUrl             string
	RecommendedPartSize     int
	AbsoluteMinimumPartSize int
}

type responseGetUploadURL struct {
	BucketID           string `json:"bucketId"`
	UploadURL          string `json:"uploadUrl"`
	AuthorizationToken string `json:"authorizationToken"`
}

// NewB2 constructs a new B2 instance
func NewB2(keyID, appKey, path string) (*B2, error) {
	authInfo, err := authorizeAccount(keyID, appKey)
	if err != nil {
		return nil, fmt.Errorf("error authorizing keyID %s: %s", keyID, err)
	}
	b2 := B2{
		Token:   authInfo.AuthorizationToken,
		Path:    path,
		BaseURL: authInfo.ApiUrl,
	}
	return &b2, nil
}

// Store writes the bytes to B2 at b2.Path
// 1. Get the URL for uploading
// 2. Upload the datfa
func (b2 B2) Store(r io.Reader) error {
	return nil
}

func authorizeAccount(keyID, appKey string) (*responseAuthorizeAccount, error) {
	// Define Request
	authString := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", keyID, appKey)))
	r, err := http.NewRequest(http.MethodGet, authURL, nil)
	r.Header.Add("Authorization", authString)
	if err != nil {
		return nil, fmt.Errorf("error creating auth request: %s", err)
	}

	// Execute Request
	rsp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("error obtaining auth response: %s", err)
	}
	if rsp.StatusCode != 200 {
		bs, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to dump response body: %s", err)
		}
		return nil, fmt.Errorf("B2 Authorization request failed with status code %d and message %s", rsp.StatusCode, bs)
	}

	defer rsp.Body.Close()

	// JSON
	var authInfo responseAuthorizeAccount
	if err := json.NewDecoder(rsp.Body).Decode(&authInfo); err != nil {
		return nil, fmt.Errorf("error obtaining authorization info: %s", err)
	}
	return &authInfo, nil
}
