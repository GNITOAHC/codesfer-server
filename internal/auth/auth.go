package auth

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

type dbStruct struct {
	db *sql.DB
}

var (
	db  dbStruct
	dev bool

	// Constant for reserved usernames
	reservedUsername = [3]string{"anon", "admin", "root"}
)

func Init(tursoURL, tursoToken string, _dev bool) {
	dev = _dev
	if _dev {
		_db, err := sql.Open("sqlite", "file:auth.db?cache=shared")
		if err != nil {
			panic(err)
		}
		db = dbStruct{db: _db}
		err = createTable()
		if err != nil {
			panic(err)
		}
		return
	}

	conn := tursoURL + "?authToken=" + tursoToken

	_db, err := sql.Open("libsql", conn)
	if err != nil {
		panic(err)
	}

	db = dbStruct{db: _db}
	err = createTable()
	if err != nil {
		panic(err)
	}
}

func AuthHandler() http.Handler {
	authhandler := http.NewServeMux()
	authhandler.HandleFunc("GET /username", username)
	authhandler.HandleFunc("POST /register", register)
	authhandler.HandleFunc("POST /login", login)
	authhandler.HandleFunc("POST /logout", logout)
	authhandler.HandleFunc("GET /me", me)

	if dev {
		authhandler.HandleFunc("GET /users", getAllUsers)
		authhandler.HandleFunc("GET /sessions", getAllSessions)
		authhandler.HandleFunc("POST /reset", reset)
	}
	return authhandler
}

func getAllUsers(w http.ResponseWriter, r *http.Request) {
	users, err := db.getUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(users)
}

func getAllSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := db.getAllSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(sessions)
}

func reset(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	err := db.reset(table)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("database reset"))
}

// username route will check if a username is taken
func username(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	for _, reserved := range reservedUsername {
		if username == reserved {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("username forbidden"))
			return
		}
	}
	exists := db.usernameExists(username)
	if exists {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("username taken"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("username available"))
}

func register(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Username string `json:"username"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if data.Email == "" || data.Password == "" || data.Username == "" {
		http.Error(w, "email, password adn username are required", http.StatusBadRequest)
		return
	}
	log.Printf("[/auth/register] user %s is trying to register", data.Email)
	err = db.createUser(data.Email, data.Password, data.Username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[/auth/register] user created: %s", data.Email)
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("user created"))
}

func login(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Username string `json:"username"` // Unimplemented
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		if err.Error() == "user not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("[/auth/login] user %s is trying to login", data.Email)
	verified, err := verify(data.Email, data.Password)
	if err != nil {
		if err.Error() == "user not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			log.Printf("[/auth/login] [user not found] user %s failed to login", data.Email)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("[/auth/login] [internal error] user %s failed to login", data.Email)
		return
	}
	if !verified {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		log.Printf("[/auth/login] [invalid credentials] user %s failed to login", data.Email)
		return
	}
	agent := r.Header.Get("User-Agent")
	ip := r.RemoteAddr
	ip = strings.Split(ip, ":")[0] // Remove port
	sessionID, err := db.createSession(data.Email, agent, ip)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Session-ID", sessionID)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sessionID))
}

func logout(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Authorization")
	if sessionID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID = sessionID[7:] // Remove "Bearer "

	err := db.deleteSession(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("logout success"))
}

func me(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := db.getUserFromSessionID(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessions, err := db.getSessions(user.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var respSessions []struct {
		Location  string `json:"location"`
		Agent     string `json:"agent"`
		LastSeen  string `json:"last_seen"`
		CreatedAt string `json:"created_at"`
		Current   bool   `json:"current"`
	}

	for _, session := range sessions {
		respSessions = append(respSessions, struct {
			Location  string `json:"location"`
			Agent     string `json:"agent"`
			LastSeen  string `json:"last_seen"`
			CreatedAt string `json:"created_at"`
			Current   bool   `json:"current"`
		}{
			Location:  session.Location,
			Agent:     session.Agent,
			LastSeen:  session.LastSeen,
			CreatedAt: session.CreatedAt,
			Current:   session.ID == sessionID,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"email":    user.Email,
		"username": user.Username,
		"sessions": respSessions,
	})
}
