package backend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const AUTH_URL = "https://api.backblazeb2.com/b2api/v2/b2_authorize_account"
const API_VERSION = "v2"

type B2 struct {
	AccountAuthorizationAPIToken string // auth token for API calls - valid for 24h
	APIUrl                       string // for API calls (not uploading/downloading)
	Filename                     string // filename of the object to upload/download
	FilenamePrefix               string // bucket-configured filename prefix (app key security restriction)
	BucketName                   string // Bucket on which to perform operations
	Client                       *s3.S3 // S3 Client
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

// NewB2 constructs a new B2 instance
func NewB2(keyID, appKey, path string) (*B2, error) {
	authInfo, err := authorizeAccount(keyID, appKey)
	if err != nil {
		return nil, fmt.Errorf("error authorizing keyID %s: %s", keyID, err)
	}
	if authInfo.Allowed.BucketName == "" {
		return nil, fmt.Errorf("failed to retrieve bucket name using application key: %s", keyID)
	}

	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(keyID, appKey, ""),
		Endpoint:         aws.String("https://s3.us-west-000.backblazeb2.com"),
		Region:           aws.String("us-west-000"),
		S3ForcePathStyle: aws.Bool(true),
	}

	newSession, err := session.NewSession(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %s", err)
	}

	s3Client := s3.New(newSession)

	b2 := B2{
		Filename:       path,
		FilenamePrefix: authInfo.Allowed.NamePrefix,
		BucketName:     authInfo.Allowed.BucketName,
		Client:         s3Client,
	}

	return &b2, nil
}

// Store writes the bytes to B2 at b2.Path
// 1. Get the URL for uploading
// 2. Upload the datfa
func (b2 B2) Store(bs []byte) error {
	buf := bytes.NewReader(bs)

	bucketName := aws.String(b2.BucketName)
	objectKey := aws.String(b2.FilenamePrefix + b2.Filename)

	_, err := b2.Client.PutObject(&s3.PutObjectInput{
		Body:   buf,
		Bucket: bucketName,
		Key:    objectKey,
	})
	if err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}
	return nil
}

func (b2 B2) Lock() error {
	bucketName := aws.String(b2.BucketName)
	objectKey := aws.String(b2.FilenamePrefix + b2.Filename)

	_, err := b2.Client.PutObjectLegalHold(&s3.PutObjectLegalHoldInput{
		Bucket: bucketName,
		Key:    objectKey,
		LegalHold: &s3.ObjectLockLegalHold{
			Status: aws.String(s3.ObjectLockLegalHoldStatusOn),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to lock file: %v", err)
	}
	return nil
}

func (b2 B2) Unlock() error {
	bucketName := aws.String(b2.BucketName)
	objectKey := aws.String(b2.FilenamePrefix + b2.Filename)

	_, err := b2.Client.PutObjectLegalHold(&s3.PutObjectLegalHoldInput{
		Bucket: bucketName,
		Key:    objectKey,
		LegalHold: &s3.ObjectLockLegalHold{
			Status: aws.String(s3.ObjectLockLegalHoldStatusOff),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to lock file: %v", err)
	}
	return nil
}

func (b2 B2) DeleteLockedFile(name, id string) error {
	if _, err := b2.Client.PutObjectLegalHold(&s3.PutObjectLegalHoldInput{
		Bucket: aws.String(b2.BucketName),
		Key:    aws.String(name),
		LegalHold: &s3.ObjectLockLegalHold{
			Status: aws.String(s3.ObjectLockLegalHoldStatusOff),
		},
	}); err != nil {
		return fmt.Errorf("failed to lock file: %v", err)
	}
	if _, err := b2.Client.DeleteObject(&s3.DeleteObjectInput{
		Bucket:    aws.String(b2.BucketName),
		Key:       aws.String(name),
		VersionId: aws.String(id),
	}); err != nil {
		return fmt.Errorf("failed to delete file: %v", err)
	}
	return nil
}
