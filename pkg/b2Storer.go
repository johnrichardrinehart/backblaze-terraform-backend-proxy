package backend

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
)

const AUTH_URL = "https://api.backblazeb2.com/b2api/v2/b2_authorize_account"
const API_VERSION = "v2"

type B2 struct {
	AccountAuthorizationAPIToken string // auth token for API calls - valid for 24h
	APIUrl                       string // for API calls (not uploading/downloading)
	Filename                     string // filename of the object to upload/download
	FilenamePrefix               string // bucket-configured filename prefix (app key security restriction)
	BucketID                     string // Bucket on which to perform operations
	// DownloadURL                  string // for downloading files (upload URLs are obtained from get_upload_url)
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
	BucketID                 string `json:"bucketId"`
	UploadURL                string `json:"uploadUrl"`
	UploadAuthorizationToken string `json:"authorizationToken"`
}

type responseUploadFile struct {
	AccountId            string
	Action               string // start/upload/hide/folder
	BucketId             string
	ContentLength        uint64
	ContentSha1          string
	ContentMd5           string `json:",omitempty"`
	ContentType          string
	FileId               string
	FileInfo             map[string]interface{}
	FileName             string
	ServerSideEncryption string // SSE-B2/SSE-C + algo
	UploadTimestamp      uint64 // uint64, ms since Unix epoch
}

// NewB2 constructs a new B2 instance
func NewB2(keyID, appKey, path string) (*B2, error) {
	authInfo, err := authorizeAccount(keyID, appKey)
	if err != nil {
		return nil, fmt.Errorf("error authorizing keyID %s: %s", keyID, err)
	}

	b2 := B2{
		AccountAuthorizationAPIToken: authInfo.AuthorizationToken,
		APIUrl:                       authInfo.ApiUrl,
		Filename:                     path,
		FilenamePrefix:               authInfo.Allowed.NamePrefix,
		BucketID:                     authInfo.Allowed.BucketID,
		// DownloadURL:                  authInfo.DownloadUrl,
	}
	return &b2, nil
}

// Store writes the bytes to B2 at b2.Path
// 1. Get the URL for uploading
// 2. Upload the datfa
func (b2 B2) Store(bs []byte) error {
	upURLInfo, err := b2.getUploadURL()
	if err != nil {
		return fmt.Errorf("failed to obtain upload URL: %s", err)
	}

	_, err = b2.uploadFile(upURLInfo.UploadAuthorizationToken, upURLInfo.UploadURL, bs)
	if err != nil {
		return err
	}
	return nil
}

func (b2 B2) getUploadURL() (*responseGetUploadURL, error) {
	// Define request
	url, err := generateAPIPath(b2.APIUrl, API_VERSION, "get_upload_url")
	if err != nil {
		return nil, fmt.Errorf("failed to generate API path for get_upload_url: %s", err)
	}
	buf := bytes.NewBuffer([]byte(fmt.Sprintf(`{"bucketId":"%s"}`, b2.BucketID)))
	r, err := http.NewRequest(http.MethodPost, url, buf)
	r.Header.Add("Authorization", b2.AccountAuthorizationAPIToken)
	if err != nil {
		return nil, fmt.Errorf("error creating get_upload_url request: %s", err)
	}

	// Execute Request
	rsp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("error obtaining get_upload_url response: %s", err)
	}

	// Check for problems
	if rsp.StatusCode != 200 {
		bs, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to dump response body: %s", err)
		}
		return nil, fmt.Errorf("B2 get_upload_url request failed with status code %d and message %s", rsp.StatusCode, bs)
	}

	defer rsp.Body.Close()

	// JSON
	var getUploadURL responseGetUploadURL
	if err := json.NewDecoder(rsp.Body).Decode(&getUploadURL); err != nil {
		return nil, fmt.Errorf("error obtaining URL for uploads: %s", err)
	}
	return &getUploadURL, nil
}

func (b2 B2) uploadFile(uploadToken, url string, data []byte) (*responseUploadFile, error) {
	// Define Request
	r, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("error creating auth request: %s", err)
	}
	r.Header.Add("Authorization", uploadToken)
	r.Header.Add("X-Bz-File-Name", b2.FilenamePrefix+"/"+b2.Filename)
	r.Header.Add("Content-Type", "application/octet-stream")
	r.Header.Add("Content-Length", strconv.Itoa(len(data)))
	h := sha1.New()
	if _, err = h.Write(data); err != nil {
		return nil, fmt.Errorf("failed to generated SHA-1 sum for upload file: %s", err)
	}
	r.Header.Add("X-Bz-Content-Sha1", hex.EncodeToString(h.Sum(nil)))

	// Execute Request
	rsp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("error obtaining upload file response: %s", err)
	}

	// Check for problems
	if rsp.StatusCode != 200 {
		bs, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to dump response body: %s", err)
		}
		return nil, fmt.Errorf("B2 upload file request failed with status code %d and message %s", rsp.StatusCode, bs)
	}

	// JSON
	var uploadInfo responseUploadFile
	if err := json.NewDecoder(rsp.Body).Decode(&uploadInfo); err != nil {
		return nil, fmt.Errorf("error obtaining upload file response info: %s", err)
	}
	return &uploadInfo, nil
}

func authorizeAccount(keyID, appKey string) (*responseAuthorizeAccount, error) {
	// Define Request
	authString := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", keyID, appKey)))
	r, err := http.NewRequest(http.MethodGet, AUTH_URL, nil)
	r.Header.Add("Authorization", authString)
	if err != nil {
		return nil, fmt.Errorf("error creating auth request: %s", err)
	}

	// Execute Request
	rsp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("error obtaining auth response: %s", err)
	}

	// Check for problems
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

func generateAPIPath(base, version, endpoint string) (string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/b2api/%s/b2_%s", base, version, endpoint))
	if err != nil {
		return "", fmt.Errorf("unable to generate API path: %s", err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return u.String(), nil
}
