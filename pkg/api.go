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

var ErrNotLocked = errors.New("error writing - resource not locked")

type Storer interface {
	Store([]byte) error  // Store file in bucket
	Lock(string) error   // Lock bucket file
	Unlock(string) error // Unlock bucket file
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
	// bs, _ := httputil.DumpRequest(r, true)
	// log.Printf("%s", bs)

	switch r.Method {
	case http.MethodGet:
		_, err := p.getState(r)
		if err != nil {
			log.Printf("failed to retrieve state: %s", err)
		}
	case http.MethodPost:
		if len(r.URL.Query()["ID"]) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		id := r.URL.Query()["ID"][0]

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

		if err := p.postState(id, md5sum, int64(l), r.Body); err != nil {
			log.Printf("failed to post state: %s", err)
		}
	case "LOCK":
		if err := p.lockState(r.Body); err != nil {
			log.Printf("failed to lock state: %s", err)
		}
	case "UNLOCK":
		if err := p.unlockState(r.Body); err != nil {
			log.Printf("failed to lock state: %s", err)
		}
	}

}

func (*Proxy) getState(r *http.Request) ([]byte, error) {
	log.Println("getting state")

	return nil, nil
}

func (p *Proxy) postState(id, md5sum string, n int64, body io.Reader) error {
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

	return p.Store(bs)
}

func (p *Proxy) lockState(body io.Reader) error {
	log.Println("locking state")

	var l Lock

	if err := json.NewDecoder(body).Decode(&l); err != nil {
		return err
	}

	return p.Lock(l.Path)
}

func (p *Proxy) unlockState(body io.Reader) error {
	log.Println("unlocking state")

	var l Lock

	if err := json.NewDecoder(body).Decode(&l); err != nil {
		return err
	}

	return p.Unlock(l.Path)
}
