package backend

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"
)

var ErrNoExist = errors.New("file does not exist (yet!)")
var ErrNotLocked = errors.New("error writing - resource not locked")

type Storer interface {
	Store(Object) error         // Store file in bucket
	Retrieve() (*Object, error) // Retrieve file from bucket
	Lock(string) error          // Lock bucket file
	Unlock(string) error        // Unlock bucket file
}

type Object struct {
	LockID string `json:"lock_id"`
	State  []byte `json:"state"`
}

type Proxy struct {
	Storer
	*http.Server
}

type Lock struct {
	ID        string
	Operation string
	Info      string
	Who       string
	Version   string
	Created   time.Time
	Path      string
}

func NewServer(proxyAddress string, storer Storer) (*Proxy, error) {
	p := &Proxy{
		Server: nil,
	}
	// TODO: move http.Server to the top of NewServer, blocked by Handler field
	s := &http.Server{
		Addr:    proxyAddress,
		Handler: http.HandlerFunc(p.handle),
	}
	p.Server = s

	if storer == nil {
		return nil, errors.New("provided Storer must be non-nil")
	}
	p.Storer = storer
	return p, nil
}

func (p *Proxy) Start() error {
	log.Printf("backblaze proxy starting at: %s", p.Addr)

	if err := p.Server.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func (p *Proxy) Shutdown(ctx context.Context) error {

	log.Printf("backblaze proxy shutting down")

	if err := p.Server.Shutdown(ctx); err != nil {
		return err
	}

	return nil
}

func (p *Proxy) handle(w http.ResponseWriter, r *http.Request) {

	switch r.Method {
	case http.MethodGet:
		bs, err := p.getState(r)
		if err != nil {
			log.Printf("failed to retrieve state: %s", err)
		}
		if n, err := w.Write(bs); n != len(bs) || err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			if n == len(bs) {
				log.Printf("failed to write response: %s", err)
				return
			}
			log.Printf("failed to write %d bytes, only wrote %d", len(bs), n)
		}
	case http.MethodPost:
		if len(r.URL.Query()["ID"]) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		lockID := r.URL.Query().Get("ID") // Lock ID

		md5sum := r.Header.Get("content-md5") // case-insensitive
		if md5sum == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		ls := r.Header.Get("content-length") // case-insensitive
		if ls == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		l, err := strconv.ParseInt(ls, 10, 64) // base-10, 64 bits

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := p.postState(lockID, md5sum, int64(l), r.Body); err != nil {
			log.Printf("failed to post state: %s", err)
		}
	case "LOCK":
		var l Lock
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			log.Printf("failed to JSON-decode body of terraform lock request: %s", err)
		}
		if err := p.lockState(l.ID); err != nil {
			log.Printf("failed to lock state: %s", err)
		}
	case "UNLOCK":
		var l Lock
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			log.Printf("failed to JSON-decode body of terraform unlock request: %s", err)
		}
		if err := p.unlockState(l.ID); err != nil {
			log.Printf("failed to unlock state: %s", err)
		}
	}

}

func (p *Proxy) getState(r *http.Request) ([]byte, error) {
	log.Println("getting state")

	obj, err := p.Retrieve()
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %s", err)
	}

	return obj.State, nil
}

func (p *Proxy) postState(lockID, md5sum string, n int64, body io.Reader) error {
	log.Println("posting state")

	// read the body
	bs, err := ioutil.ReadAll(body)
	if err != nil {
		return fmt.Errorf("failed to read body: %s", err)
	}

	// md5-hash it
	h := md5.New()
	if n, err := h.Write(bs); n != len(bs) || err != nil {
		if n != len(bs) {
			return fmt.Errorf("failed to generate md5 sum of file: %d bytes expected to be written - %d actually written", len(bs), n)
		}
		return fmt.Errorf("failed to generate md5 sum of file: %s", err)
	}

	// base64-ify the md5 hash
	calcSum := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if calcSum != md5sum {
		log.Printf("Input: %s\nCalculated: %s", md5sum, calcSum)
		return errors.New("invalid checksum")
	}

	// check that we have the lock to execute the update
	if lockID != "" {
		obj, err := p.Retrieve()
		if err != nil {
			return fmt.Errorf("failed to retrieve document for updating: %s", err)
		}

		if lockID != obj.LockID {
			return fmt.Errorf("unallowed to unlock, your lockID is %s and you need %s", lockID, obj.LockID)
		}
	}

	obj := Object{
		LockID: lockID,
		State:  bs,
	}

	if err := p.Store(obj); err != nil {
		return fmt.Errorf("failed to store file: %s", err)
	}

	return nil
}

func (p *Proxy) lockState(id string) error {
	log.Println("locking state")

	// check that it's not currently locked
	obj, err := p.Retrieve()
	if err != nil {
		// new state file, need to create an empty one
		if err == ErrNoExist {
			obj := Object{
				LockID: id,
				State:  nil,
			}
			if err := p.Storer.Store(obj); err != nil {
				return fmt.Errorf("failed to create and lock new state file: %s", err)
			}
			return nil
		}
		return fmt.Errorf("failed to retrieve document for updating: %s", err)
	}
	if obj.LockID != "" {
		return fmt.Errorf("unable to lock with id %s- file currently locked with lock ID %s", id, obj.LockID)
	}

	return p.Lock(id)
}

func (p *Proxy) unlockState(id string) error {
	log.Println("unlocking state")

	// check that it's currently locked by us
	obj, err := p.Retrieve()
	if err != nil {
		return fmt.Errorf("failed to retrieve document for updating: %s", err)
	}
	if obj.LockID != id {
		return fmt.Errorf("unable to unlock with lock ID %s - file currently locked with lock ID %s", id, obj.LockID)
	}
	return p.Unlock(id)
}
