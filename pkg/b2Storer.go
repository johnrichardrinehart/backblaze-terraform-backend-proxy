package backend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const AUTH_URL = "https://api.backblazeb2.com/b2api/v2/b2_authorize_account"
const API_VERSION = "v2"

type B2 struct {
	AccountAuthorizationAPIToken string // auth token for API calls - valid for 24h
	APIUrl                       string // for API calls (not uploading/downloading)
	Key                          string // location of state file (prefix + constructor-passed-path)
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
		Key:        authInfo.Allowed.NamePrefix + path,
		BucketName: authInfo.Allowed.BucketName,
		Client:     s3Client,
	}

	return &b2, nil
}

func (b2 B2) Retrieve() (*Object, error) {
	out, err := b2.Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(b2.BucketName),
		Key:    &b2.Key,
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			// No document exists
			if awsErr.Code() == s3.ErrCodeNoSuchKey {
				return nil, ErrNoExist
			}
		}
		return nil, fmt.Errorf("failed to retrieve object: %s", err)
	}

	var obj Object
	if err := json.NewDecoder(out.Body).Decode(&obj); err != nil {
		return nil, fmt.Errorf("failed to parse remote state object: %s", err)
	}

	return &obj, nil
}

// Store writes the bytes to B2 at b2.Path
// 1. Get the URL for uploading
// 2. Upload the datfa
func (b2 B2) Store(obj Object) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(obj); err != nil {
		return fmt.Errorf("failed to marshal object for storing: %s", err)
	}

	rdr := bytes.NewReader(buf.Bytes())

	if _, err := b2.Client.PutObject(&s3.PutObjectInput{
		Body:   rdr,
		Bucket: &b2.BucketName,
		Key:    &b2.Key,
	}); err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}

	return nil
}

func (b2 B2) Lock(id string) error {
	out, err := b2.Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(b2.BucketName),
		Key:    &b2.Key,
	})
	if err != nil {
		return fmt.Errorf("failed to retrieve object to check lock status: %s", err)
	}

	var obj Object
	if err := json.NewDecoder(out.Body).Decode(&obj); err != nil {
		return fmt.Errorf("failed to parse remote state object: %s", err)
	}

	if obj.LockID != "" && obj.LockID != id {
		return fmt.Errorf("unable to lock with id %s - currently locked by id %s", id, obj.LockID)
	}

	obj.LockID = id

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(obj); err != nil {
		return fmt.Errorf("unable to generate JSON payload for locking: %s", err)
	}

	b2.Client.PutObject(&s3.PutObjectInput{
		Body:   bytes.NewReader(buf.Bytes()),
		Bucket: &b2.BucketName,
		Key:    &b2.Key,
	})

	if err != nil {
		return fmt.Errorf("failed to marhal JSON for locking: %s", err)
	}

	return nil
}

func (b2 B2) Unlock(id string) error {

	out, err := b2.Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(b2.BucketName),
		Key:    &b2.Key,
	})
	if err != nil {
		return fmt.Errorf("failed to retrieve object to check lock status: %s", err)
	}

	var obj Object
	if err := json.NewDecoder(out.Body).Decode(&obj); err != nil {
		return fmt.Errorf("failed to parse remote state object: %s", err)
	}

	if obj.LockID != "" && obj.LockID != id {
		return fmt.Errorf("unable to unlock using id %s - currently locked by id %s", id, obj.LockID)
	}

	obj.LockID = "" // unlock it

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(obj); err != nil {
		return fmt.Errorf("unable to generate JSON payload for locking: %s", err)
	}

	b2.Client.PutObject(&s3.PutObjectInput{
		Body:   bytes.NewReader(buf.Bytes()),
		Bucket: &b2.BucketName,
		Key:    &b2.Key,
	})

	if err != nil {
		return fmt.Errorf("failed to marshal JSON for locking: %s", err)
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
