package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Global State
var (
	isAppReady   = false 
	isAppLive    = false
	memoryStore  [][]byte
	config       Config
)

// Config 구조체
type Config struct {
	Role               string
	Version            string
	Profile            string
	PathPersistent     string
	PathPod            string
	PostgresqlFilepath string
	DbDriver           string
	DbUrl              string
	DbUser             string
	DbPass             string
}

// YAML 매핑용 구조체
type SecretYaml struct {
	DriverClassName string `yaml:"driver-class-name"`
	Url             string `yaml:"url"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
}

func init() {	
	config = loadConfig()
	datasourceSecretLoad()
}

// --- Pod Logic (In-Memory Only for Target Pod Demo) ---

type Pod struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	HP       int       `json:"hp"`
}

var (
	pods     []Pod
	podsLock sync.RWMutex
)

func init() {
	// Initialize with some dummy data for API testing
	podsLock.Lock()
	defer podsLock.Unlock()
	pods = []Pod{
		{ID: "1", Name: "nginx-1", Status: "Running", HP: 5},
	}
}

func main() {
	// 1. Probes
	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if isAppReady { w.Write([]byte("ok")) } else { w.WriteHeader(500) }
	})
	http.HandleFunc("/liveness", func(w http.ResponseWriter, r *http.Request) {
		if isAppLive { w.Write([]byte("ok")) } else { w.WriteHeader(500) }
	})
	http.HandleFunc("/startup", func(w http.ResponseWriter, r *http.Request) {
		if isAppLive { w.Write([]byte("ok")) } else { w.WriteHeader(500) }
	})

	// 2. API Endpoints
	http.Handle("/api/pods", enableCORS(http.HandlerFunc(handlePods)))

	fmt.Println("Starting HTTP Server on port 8080...")
	
	isAppLive = true
	isAppReady = true
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handlePods(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case "GET":
		podsLock.RLock()
		json.NewEncoder(w).Encode(pods)
		podsLock.RUnlock()
	case "POST":
		// Simple create for testing
		id := strconv.FormatInt(time.Now().UnixNano(), 10)
		newPod := Pod{ID: id, Name: "pod-" + id[len(id)-4:], Status: "Running", HP: 5}
		podsLock.Lock()
		pods = append(pods, newPod)
		podsLock.Unlock()
		json.NewEncoder(w).Encode(newPod)
	}
}

// Helpers
func loadConfig() Config {
	return Config{
		Role:               getEnv("APPLICATION_ROLE", "ALL"),
		Version:            getEnv("APPLICATION_VERSION", "Go-App v1.0.0"),
		Profile:            getEnv("SPRING_PROFILES_ACTIVE", "default"),
		PostgresqlFilepath: getEnv("POSTGRESQL_FILEPATH", "/etc/config/postgresql.yaml"),
	}
}

func datasourceSecretLoad() {} // Empty for now
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists { return value }
	return fallback
}