package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.seankhliao.com/svcrunner"
	"go.seankhliao.com/svcrunner/envflag"
	"go.seankhliao.com/webstyle"
	"go.seankhliao.com/webstyle/webstatic"
)

//go:embed index.md
var indexRaw []byte

type Server struct {
	bucket string
	bkt    *storage.BucketHandle
	index  []byte

	log   logr.Logger
	trace trace.Tracer
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
	c.StringVar(&s.bucket, "paste.bucket", "", "storage bucket")
}

func (s *Server) Init(ctx context.Context, t svcrunner.Tools) error {
	s.log = t.Log.WithName("paste")
	s.trace = otel.Tracer("paste")

	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	s.bkt = client.Bucket(s.bucket)

	render := webstyle.NewRenderer(webstyle.TemplateCompact)
	s.index, err = render.RenderBytes(indexRaw, webstyle.Data{})
	if err != nil {
		return fmt.Errorf("render index: %w", err)
	}
	return nil
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	_, span := s.trace.Start(r.Context(), "index")
	defer span.End()

	if r.URL.Path != "/" {
		http.Redirect(rw, r, "/", http.StatusFound)
		return
	}
	http.ServeContent(rw, r, "index.html", time.Unix(0, 0), bytes.NewReader(s.index))
}

func (s *Server) lookup(rw http.ResponseWriter, r *http.Request) {
	objName := r.URL.Path[1:]
	log := s.log.WithValues("handler", "lookup", "bucket", s.bucket, "object", objName)
	ctx, span := s.trace.Start(r.Context(), "lookup")
	defer span.End()

	for _, etag := range r.Header.Values("if-none-match") {
		if etag == objName {
			rw.WriteHeader(http.StatusNotModified)
			log.V(1).Info("etag matched")
			return
		}
	}

	obj := s.bkt.Object(objName)
	or, err := obj.NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		http.Error(rw, "not found", http.StatusNotFound)
		log.V(1).Info("object not found")
		return
	} else if err != nil {
		http.Error(rw, "get object", http.StatusNotFound)
		log.Error(err, "get object reader")
		return
	}
	defer or.Close()

	rw.Header().Set("cache-control", "max-age=31536000") // 365 days
	rw.Header().Set("etag", objName)

	_, err = io.Copy(rw, or)
	if err != nil {
		log.Error(err, "copy from bucket")
	}
	log.V(1).Info("served object")
}

func (s *Server) upload(rw http.ResponseWriter, r *http.Request) {
	log := s.log.WithValues("handler", "upload")
	ctx, span := s.trace.Start(r.Context(), "upload")
	defer span.End()

	// request validation
	if r.Method != http.MethodPost {
		http.Error(rw, "POST only", http.StatusMethodNotAllowed)
		log.V(1).Info("invalid method", "method", r.Method)
		return
	}
	if r.URL.Path != "/paste/" {
		http.Error(rw, "not found", http.StatusNotFound)
		log.V(1).Info("invalid path", "path", r.URL.Path)
		return
	}

	var val []byte
	var uploadSource string
	ct := r.Header.Get("content-type")
	switch {
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		val = []byte(r.FormValue("paste"))
		uploadSource = "form"
	case strings.HasPrefix(ct, "multipart/form-data"):
		err := r.ParseMultipartForm(1 << 22) // 4M
		if err != nil {
			http.Error(rw, "bad multipart form", http.StatusBadRequest)
			log.Error(err, "parse multipart form")
			return
		}
		mpf, _, err := r.FormFile("upload")
		if err != nil {
			http.Error(rw, "bad multipart form", http.StatusBadRequest)
			log.Error(err, "get form file")
			return
		}
		defer mpf.Close()
		var buf bytes.Buffer
		_, err = io.Copy(&buf, mpf)
		if err != nil {
			http.Error(rw, "read", http.StatusInternalServerError)
			log.Error(err, "read form file")
			return
		}
		val = buf.Bytes()
		uploadSource = "file"
	default:
		http.Error(rw, "unknown content type", http.StatusBadRequest)
		log.Error(errors.New("unhandled content type"), "unknown upload", "content_type", ct)
		return
	}

	sum := sha256.Sum256(val)
	sum2 := base64.URLEncoding.EncodeToString(sum[:])
	key := path.Join("p", sum2[:8])

	log = log.WithValues("source", uploadSource, "size", len(val), "key", key)

	ctx, span = s.trace.Start(ctx, "write-object")
	defer span.End()

	obj := s.bkt.Object(key)
	ow := obj.NewWriter(ctx)
	defer ow.Close()

	_, err := io.Copy(ow, bytes.NewReader(val))
	if err != nil {
		http.Error(rw, "write", http.StatusInternalServerError)
		log.Error(err, "upload object")
	}

	fmt.Fprintf(rw, "https://%s/%s\n", r.Host, key)
	log.V(1).Info("uploaded object")
}
