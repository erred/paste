package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"go.seankhliao.com/svcrunner"
	"go.seankhliao.com/svcrunner/envflag"
	"go.seankhliao.com/webstyle"
	"go.seankhliao.com/webstyle/webstatic"
)

//go:embed index.md
var indexRaw []byte

type Server struct {
	dir   string
	index []byte
	log   logr.Logger
}

func New(hs *http.Server) *Server {
	s := &Server{}
	mux := http.NewServeMux()
	mux.Handle("/", s)
	mux.HandleFunc("/p/", s.lookup)
	mux.HandleFunc("/paste/", s.upload)
	webstatic.Register(mux)
	hs.Handler = mux
	return s
}

func (s *Server) Register(c *envflag.Config) {
	c.StringVar(&s.dir, "paste.dir", "/data", "storage directory")
}

func (s *Server) Init(ctx context.Context, t svcrunner.Tools) error {
	s.log = t.Log.WithName("paste")

	render := webstyle.NewRenderer(webstyle.TemplateCompact)

	var err error
	s.index, err = render.RenderBytes(indexRaw, webstyle.Data{})
	if err != nil {
		return fmt.Errorf("render index: %w", err)
	}
	return nil
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Redirect(rw, r, "/", http.StatusFound)
		return
	}
	http.ServeContent(rw, r, "index.html", time.Unix(0, 0), bytes.NewReader(s.index))
}

func (s *Server) lookup(rw http.ResponseWriter, r *http.Request) {
	http.ServeFile(rw, r, filepath.Join(s.dir, r.URL.Path))
}

func (s *Server) upload(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/paste/" {
		http.Error(rw, "not found", http.StatusNotFound)
		return
	}
	val := []byte(r.FormValue("paste"))
	if len(val) == 0 {
		err := r.ParseMultipartForm(1 << 22) // 4M
		if err != nil {
			s.log.Error(err, "parse multipart form")
			http.Error(rw, "bad multipart form", http.StatusBadRequest)
			return
		}
		mpf, _, err := r.FormFile("upload")
		if err != nil {
			s.log.Error(err, "get form file")
			http.Error(rw, "bad multipart form", http.StatusBadRequest)
			return
		}
		defer mpf.Close()
		var buf bytes.Buffer
		_, err = io.Copy(&buf, mpf)
		if err != nil {
			s.log.Error(err, "read form file")
			http.Error(rw, "read", http.StatusInternalServerError)
			return
		}
		val = buf.Bytes()
	}

	sum := sha256.Sum256(val)
	sum2 := base64.URLEncoding.EncodeToString(sum[:])

	key := path.Join("p", sum2[:8])
	err := os.WriteFile(filepath.Join(s.dir, key), val, 0o644)
	if err != nil {
		s.log.Error(err, "write file", "file", key)
		http.Error(rw, "write", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, "https://%s/%s\n", r.Host, key)
}
