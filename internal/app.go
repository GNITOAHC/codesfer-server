package internal

import (
	"codeserver/internal/auth"
	"codeserver/internal/r2"
	"codeserver/internal/storage"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/gnitoahc/go-dotenv"
)

var (
	port = flag.Int("port", 3000, "The server port")
)

func init() {
	dotenv.Load(".env")

	// r2 package init
	CF_ACCOUNT_ID := os.Getenv("CF_ACCOUNT_ID")
	CF_BUCKET_NAME := os.Getenv("CF_BUCKET_NAME")
	CF_ACCESS_KEY := os.Getenv("CF_ACCESS_KEY")
	CF_SECRET_ACCESS_KEY := os.Getenv("CF_SECRET_ACCESS_KEY")
	r2.Init(CF_ACCOUNT_ID, CF_ACCESS_KEY, CF_SECRET_ACCESS_KEY, CF_BUCKET_NAME)

	// auth package init
	auth.Init("", "", true)

	// storage package init
	storage.Init("", "", true)
}

func Serve() {
	// Mux definition start
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	})
	handle(mux, "/auth/", http.StripPrefix("/auth", auth.AuthHandler()))
	handle(mux, "/storage/", http.StripPrefix("/storage", storage.StorageHandler()), authMiddleware)
	handle(mux, "/anonymous/", http.StripPrefix("/anonymous", storage.AnonymousHandler()))
	// Mux definition end

	log.Printf("Starting server on port %d", *port)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Fatal(http.Serve(lis, mux))
}
