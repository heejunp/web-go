package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"

	"strconv"
	"strings"
	"sync"
	"time"

	pb "web-go/proto"

	"google.golang.org/grpc"

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

// --- Pod & Pub/Sub Logic ---

type Pod struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Position []float64 `json:"position"`
	HP       int       `json:"hp"`
}

var (
	pods     []Pod
	podsLock sync.RWMutex
	
	// gRPC Streaming Clients
	grpcClients sync.Map // map[chan *pb.PodEvent]bool
)

func init() {
	// Initialize with some dummy data
	podsLock.Lock()
	defer podsLock.Unlock()
	pods = []Pod{
		{ID: "1", Name: "nginx-1", Status: "Running", Position: []float64{-2, 0, -2}, HP: 5},
		{ID: "2", Name: "redis-1", Status: "Pending", Position: []float64{2, 0, -2}, HP: 5},
		{ID: "3", Name: "api-svr", Status: "Failed", Position: []float64{-2, 0, 2}, HP: 5},
		{ID: "4", Name: "test-target", Status: "Running", Position: []float64{2, 0, 2}, HP: 5},
	}

	// [Self-Healing Simulation]
	go func() {
		for {
			time.Sleep(3 * time.Second)
			
			podsLock.Lock()
			currentCount := len(pods)
			targetCount := 4 

			if currentCount < targetCount {
				id := strconv.FormatInt(time.Now().UnixNano(), 10)
				newPod := Pod{
					ID:     id,
					Name:   "replica-" + id[len(id)-4:],
					Status: "Pending",
					Position: []float64{
						(rand.Float64() - 0.5) * 6,
						0,
						(rand.Float64() - 0.5) * 6,
					},
					HP: 5,
				}
				pods = append(pods, newPod)
				fmt.Printf("[ReplicaSet] Respawned: %s\n", newPod.Name)
				
				// Broadcast ADDED
				broadcastEvent("ADDED", newPod)

				// Simulate startup delay
				go func(pID string) {
					time.Sleep(2 * time.Second)
					podsLock.Lock()
					defer podsLock.Unlock()
					for i := range pods {
						if pods[i].ID == pID {
							pods[i].Status = "Running"
							// Broadcast MODIFIED
							broadcastEvent("MODIFIED", pods[i])
							break
						}
					}
				}(id)
			}
			podsLock.Unlock()
		}
	}()
}

// Broadcasts event to all gRPC streams
func broadcastEvent(eventType string, p Pod) {
	evt := &pb.PodEvent{
		Type:     eventType,
		Id:       p.ID,
		Name:     p.Name,
		Status:   p.Status,
		Hp:       int32(p.HP),
		Position: p.Position,
	}
	
	grpcClients.Range(func(key, value interface{}) bool {
		ch := key.(chan *pb.PodEvent)
		select {
		case ch <- evt:
		default:
			// Client lagging?
		}
		return true
	})
}

// --- gRPC Server Implementation ---

type podServer struct {
	pb.UnimplementedPodServiceServer
}

func (s *podServer) Subscribe(in *pb.Empty, stream pb.PodService_SubscribeServer) error {
	log.Println("[gRPC] New Subscriber connected")
	
	// Send Initial State
	podsLock.RLock()
	for _, p := range pods {
		evt := &pb.PodEvent{
			Type:     "ADDED",
			Id:       p.ID,
			Name:     p.Name,
			Status:   p.Status,
			Hp:       int32(p.HP),
			Position: p.Position,
		}
		if err := stream.Send(evt); err != nil {
			podsLock.RUnlock()
			return err
		}
	}
	podsLock.RUnlock()

	// Create channel for this client
	ch := make(chan *pb.PodEvent, 100)
	grpcClients.Store(ch, true)
	defer grpcClients.Delete(ch)

	// Stream Loop
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case evt := <-ch:
			if err := stream.Send(evt); err != nil {
				return err
			}
		}
	}
}

func (s *podServer) DeletePod(ctx context.Context, in *pb.PodId) (*pb.ActionResponse, error) {
	log.Printf("[gRPC] Delete Request: %s", in.Id)
	
	podsLock.Lock()
	defer podsLock.Unlock()

	for i, p := range pods {
		if p.ID == in.Id {
			// Found -> Delete
			deletedPod := pods[i]
			pods = append(pods[:i], pods[i+1:]...)
			
			broadcastEvent("DELETED", deletedPod)
			
			return &pb.ActionResponse{Success: true, Message: "Deleted"}, nil
		}
	}
	return &pb.ActionResponse{Success: false, Message: "Not Found"}, nil
}

// --- Main ---

func main() {
	// Start gRPC Server
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		s := grpc.NewServer()
		pb.RegisterPodServiceServer(s, &podServer{})
		log.Printf("Starting gRPC Server on :50051")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Start HTTP Server (Legacy + Health)
	http.Handle("/api/pods", enableCORS(http.HandlerFunc(handlePods)))
	http.Handle("/api/pods/", enableCORS(http.HandlerFunc(handlePodDetail)))
	
	// ... (Rest of HTTP handlers omitted for brevity, but crucial kept minimalist)
	
	fmt.Println("Starting HTTP Server on port 8080...")
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
		defer podsLock.RUnlock()
		json.NewEncoder(w).Encode(pods)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handlePodDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/pods/")
	id := path

	switch r.Method {
	case "OPTIONS":
		w.WriteHeader(http.StatusOK)
		return
	case "PATCH":
		var updateData struct {
			HP *int `json:"hp"`
		}
		json.NewDecoder(r.Body).Decode(&updateData)
		
		podsLock.Lock()
		defer podsLock.Unlock()
		for i, p := range pods {
			if p.ID == id {
				if updateData.HP != nil {
					pods[i].HP = *updateData.HP
					// Broadcast Update
					broadcastEvent("MODIFIED", pods[i])
				}
				json.NewEncoder(w).Encode(pods[i])
				return
			}
		}
		http.Error(w, "Not found", 404)

	case "DELETE":
		podsLock.Lock()
		defer podsLock.Unlock()
		for i, p := range pods {
			if p.ID == id {
				deletedPod := pods[i]
				pods = append(pods[:i], pods[i+1:]...)
				broadcastEvent("DELETED", deletedPod)
				w.WriteHeader(200)
				return
			}
		}
		http.Error(w, "Not found", 404)
	}
}

// --- Service Logic Functions (Helpers) ---
func loadConfig() Config {
	return Config{
		Role:               getEnv("APPLICATION_ROLE", "ALL"),
		Version:            getEnv("APPLICATION_VERSION", "Go-App v1.0.0"),
		Profile:            getEnv("SPRING_PROFILES_ACTIVE", "default"),
		PostgresqlFilepath: getEnv("POSTGRESQL_FILEPATH", "/etc/config/postgresql.yaml"),
	}
}

func datasourceSecretLoad() {}

func createFile(path string) string { return "" }
func listFiles(path string) string { return "" }
func memoryLeak() {}
func cpuLoad(min int, thread int) {}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists { return value }
	return fallback
}

func getHostname() string { host, _ := os.Hostname(); return host }
func logInfo(probeType string, status bool) {}