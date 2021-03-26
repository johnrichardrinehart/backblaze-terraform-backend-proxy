package backend

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"
	"time"
)

var ErrLocked = errors.New("error writing - resource locked")

type Storer interface {
	Store(io.Reader) error // stores the state
}

type Proxy struct {
	keyID          string
	applicationKey string
	locker         map[string]bool
	Storer
	*http.Server
	sync.RWMutex // (un)lock locker
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

func NewServer(addr, keyID, appKey string) (*Proxy, error) {
	p := &Proxy{
		keyID:          keyID,
		applicationKey: appKey,
		locker:         make(map[string]bool),
		Server:         nil,
	}
	// TODO: move http.Server to the top of NewServer, blocked by Handler field
	s := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(p.handle),
	}
	p.Server = s

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
	bs, _ := httputil.DumpRequest(r, true)

	log.Printf("%s", bs)

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
	}
}

func (*Proxy) getState(r *http.Request) ([]byte, error) {
	log.Println("getting state\n")

	return nil, nil
}

func (p *Proxy) postState(id, md5sum string, n int64, body io.Reader) error {
	log.Println("posted state\n")

	h := md5.New()

	m, err := io.Copy(h, body)
	if err != nil {
		return err
	}
	if m != n {
		return errors.New("invalid content length")
	}

	calcSum := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if calcSum != md5sum {
		log.Printf("Input: %s\nCalculated: %s", md5sum, calcSum)
		return errors.New("invalid checksum")
	}

	p.RLock()
	if p.locker[id] {
		return ErrLocked
	}
	p.RUnlock()
	return p.Store(body)
}

func (p *Proxy) lockState(body io.Reader) error {
	log.Println("locking state")

	var l Lock

	if err := json.NewDecoder(body).Decode(&l); err != nil {
		return err
	}

	p.lock(l.ID)
	return nil
}

func (p *Proxy) lock(id string) {
	p.Lock()
	p.locker[id] = true
	p.Unlock()
}
