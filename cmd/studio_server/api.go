package main

import (
	"log"
	"net/http"

	"github.com/rs/cors"
)

type APIServer struct {
	addr string
}

func NewAPIServer(addr string) *APIServer {
	return &APIServer{
		addr: addr,
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Flush() {
	if flusher, ok := lrw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func RequestLoggerMiddleware(next http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lrw := NewLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)
		log.Printf("Method %s Path: %s, %d ", r.Method, r.URL.Path, lrw.statusCode)
	})
}

// func ClientValidationMiddleware(next http.Handler) http.HandlerFunc {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		lrw := NewLoggingResponseWriter(w)
// 		next.ServeHTTP(lrw, r)
// 		log.Printf("Method %s Path: %s, %d ", r.Method, r.URL.Path, lrw.statusCode)
// 	})
// }

func (s *APIServer) Run() error {
	router := http.NewServeMux()

	router.HandleFunc("GET /ping", PingHandler)
	router.HandleFunc("POST /{project}", PostProjectHandler)
	router.HandleFunc("PUT /{project}", RenameProjectHandler)
	router.HandleFunc("GET /{project}", GetProjectHandler)
	router.HandleFunc("GET /{project}/sync-token", GetProjectSyncTokenHandler)
	router.HandleFunc("PUT /{project}/icon", SetProjectIconHandler)
	router.HandleFunc("PUT /{project}/ignore-list", SetProjectIgnoreListHandler)
	router.HandleFunc("PUT /{project}/toggle-close", ToggleProjectCloseHandler)
	router.HandleFunc("GET /{project}/data", GetDataHandler)
	router.HandleFunc("POST /{project}/data", PostDataHandler)
	router.HandleFunc("GET /{project}/chunks", GetChunksHandler)
	router.HandleFunc("GET /{project}/stream-chunks", StreamChunksHandler)
	router.HandleFunc("POST /{project}/chunks", PostChunksHandler)
	router.HandleFunc("GET /{project}/chunks-missing", ChunksMissingHandler)
	router.HandleFunc("GET /{project}/chunks-info", GetChunksInfoHandler)
	router.HandleFunc("GET /{project}/previews", GetPreviewsHandler)
	router.HandleFunc("GET /{project}/preview", GetProjectPreview)
	router.HandleFunc("POST /{project}/previews", PostPreviewsHandler)
	router.HandleFunc("GET /{project}/previews-exist", PreviewsExistHandler)
	router.HandleFunc("GET /projects", GetProjectsHandler)

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "Clustta-Agent", "UserData"},
		AllowCredentials: true,
	})

	handlerWithLogging := RequestLoggerMiddleware(c.Handler(router))
	// handlerWithCor := c.Handler(router)

	server := http.Server{
		Addr:         s.addr,
		Handler:      handlerWithLogging,
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	log.Printf("Server has started %s", s.addr)

	return server.ListenAndServe()
}
