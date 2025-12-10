package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3" // YAML 파싱을 위한 외부 라이브러리 필요
)

// Global State
var (
	isAppReady   = false 
	isAppLive    = false
	memoryStore  [][]byte
	config       Config
)

// Config 구조체 (설정값 저장소)
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
	// 초기 설정 로드
	config = loadConfig()
	
	// Java의 @PostConstruct에 해당하는 로직
	datasourceSecretLoad()
}

func main() {
	// 1. Basic Endpoints
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Welcome to Kubernetes Another Class")
	})

	http.HandleFunc("/hostname", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, getHostname())
	})

	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "[App Version] : %s", config.Version)
	})

	// 2. Probes (Readiness, Liveness, Startup)
	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if isAppReady {
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/liveness", func(w http.ResponseWriter, r *http.Request) {
		if isAppLive {
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/startup", func(w http.ResponseWriter, r *http.Request) {
		// probeCheck("startup") 로직
		logInfo("startup", isAppLive)
		if isAppLive {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<b>[App Initialization]</b><br>DB Connected : OK<br>Spring Initialization : OK<br>Jar is Running : OK")
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	
	http.HandleFunc("/readiness", func(w http.ResponseWriter, r *http.Request) {
		// probeCheck("readiness") 로직
		logInfo("readiness", isAppReady)
		if isAppReady {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<b>[User Initialization]</b><br>Init Data : OK<br>Linkage System Check : OK<br>DB Data Validation : OK")
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	// 3. Traffic Control
	http.HandleFunc("/traffic-off", func(w http.ResponseWriter, r *http.Request) {
		isAppReady = false
		fmt.Println("[System] Traffic is forcibly stopped")
		w.Write([]byte("ok"))
	})

	http.HandleFunc("/traffic-on", func(w http.ResponseWriter, r *http.Request) {
		isAppReady = true
		fmt.Println("[System] Traffic is reconnected")
		w.Write([]byte("ok"))
	})

	http.HandleFunc("/server-error", func(w http.ResponseWriter, r *http.Request) {
		isAppLive = false
		fmt.Println("[System] An error occurred on the server")
		w.Write([]byte("ok"))
	})

	// 4. Stress Tests
	http.HandleFunc("/memory-leak", func(w http.ResponseWriter, r *http.Request) {
		isAppReady = false
		go memoryLeak()
		w.Write([]byte("Memory leak started..."))
	})

	http.HandleFunc("/cpu-load", func(w http.ResponseWriter, r *http.Request) {
		minStr := r.URL.Query().Get("min")
		threadStr := r.URL.Query().Get("thread")
		
		min, _ := strconv.Atoi(minStr)
		if min == 0 { min = 2 }
		thread, _ := strconv.Atoi(threadStr)
		if thread == 0 { thread = 10 }

		go cpuLoad(min, thread)
		w.Write([]byte(fmt.Sprintf("CPU Load started (%d min, %d threads)", min, thread)))
	})

	// 5. Info & Properties
	http.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		html := fmt.Sprintf(`
			<b>[Version] :</b> %s<br>
			<b>[Profile] :</b> %s<br>
			<b>[Role] :</b> %s (option: ALL, GET, POST, PUT, DELETE)<br>
			<b>[Database]</b><br>
			driver-class-name : %s<br>
			url : %s<br>
			username : %s<br>
			password : %s`,
			config.Version, config.Profile, config.Role,
			config.DbDriver, config.DbUrl, config.DbUser, config.DbPass)
		fmt.Fprint(w, html)
	})
	
	http.HandleFunc("/properties", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")

		html := fmt.Sprintf(`
			<b>[Application profile] : </b> %s<br>
			<b>Volume path :</b> %s<br><br>
			<b>application.yaml :</b> Common properties<br>---<br>
			datasource:<br>&nbsp;&nbsp;driver-class-name:<br>&nbsp;&nbsp;url:<br>&nbsp;&nbsp;username:<br>&nbsp;&nbsp;password:<br>
			application:<br>&nbsp;&nbsp;role:&nbsp;"ALL"<br>&nbsp;&nbsp;version:&nbsp;"Api Tester v1.0.0"<br><br>
			postgresql:<br>&nbsp;&nbsp;filepath:<br><br>
			<b>Current Config:</b><br>
			PV Path: %s<br>Pod Path: %s`,
			config.Profile, config.PathPersistent, config.PathPersistent, config.PathPod)
		fmt.Fprint(w, html)
	})

	// 6. File Operations
	http.HandleFunc("/create-file-pv", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(createFile(config.PathPersistent)))
	})
	http.HandleFunc("/list-file-pv", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(listFiles(config.PathPersistent)))
	})
	http.HandleFunc("/create-file-pod", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(createFile(config.PathPod)))
	})
	http.HandleFunc("/list-file-pod", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(listFiles(config.PathPod)))
	})

	// Server Start
	fmt.Println("Starting Go Server on port 8080...")
	
	isAppLive = true
	isAppReady = true
	
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

// --- Service Logic Functions ---

func loadConfig() Config {
	return Config{
		Role:               getEnv("APPLICATION_ROLE", "ALL"),
		Version:            getEnv("APPLICATION_VERSION", "Go-App v1.0.0"),
		Profile:            getEnv("SPRING_PROFILES_ACTIVE", "default"),
		PathPersistent:     getEnv("VOLUME_PATH_PERSISTENT_VOLUME_DATA", "./files/pv/"),
		PathPod:            getEnv("VOLUME_PATH_POD_VOLUME_DATA", "./files/pod/"),
		PostgresqlFilepath: getEnv("POSTGRESQL_FILEPATH", "/etc/config/postgresql.yaml"), // Secret 마운트 경로
		DbDriver:           "org.postgresql.Driver", // 기본값
		DbUrl:              "jdbc:postgresql://localhost:5432/db",
		DbUser:             "user",
		DbPass:             "pass",
	}
}

// @PostConstruct: YAML 파일이 있으면 읽어서 DB 설정 덮어쓰기
func datasourceSecretLoad() {
	data, err := os.ReadFile(config.PostgresqlFilepath)
	if err != nil {
		// 파일이 없으면 무시 (기본값 사용)
		return
	}
	
	var secret SecretYaml
	err = yaml.Unmarshal(data, &secret)
	if err != nil {
		fmt.Printf("Error parsing YAML: %v\n", err)
		return
	}

	// 설정 덮어쓰기
	if secret.DriverClassName != "" { config.DbDriver = secret.DriverClassName }
	if secret.Url != "" { config.DbUrl = secret.Url }
	if secret.Username != "" { config.DbUser = secret.Username }
	if secret.Password != "" { config.DbPass = secret.Password }
	
	fmt.Println("DataSource properties loaded from YAML file.")
}

func memoryLeak() {
	hostname := getHostname()
	fmt.Printf("%s : memoryLeak is starting\n", hostname)
	// 무한 루프로 메모리 할당 (Java의 ObjectForLeak 대신 byte 슬라이스 사용)
	for {
		// 1MB 씩 할당
		block := make([]byte, 1024*1024) 
		memoryStore = append(memoryStore, block)
		time.Sleep(10 * time.Millisecond) // 너무 빨리 죽지 않게 약간의 딜레이
	}
}

func cpuLoad(min int, thread int) {
	hostname := getHostname()
	duration := time.Duration(min) * time.Minute
	load := 0.8 // 80% 부하
	
	for i := 0; i < thread; i++ {
		go func(id int) {
			fmt.Printf("%s : cpuLoad thread-%d starting (%d min)\n", hostname, id, min)
			start := time.Now()
			
			// Busy Loop for specified duration
			for time.Since(start) < duration {
				// 100ms 단위로 80% 일하고 20% 쉬기
				loopStart := time.Now()
				for time.Since(loopStart).Milliseconds() < int64(100*load) {
					// Busy wait
				}
				time.Sleep(time.Duration(100*(1-load)) * time.Millisecond)
			}
		}(i)
	}
}

func createFile(path string) string {
	// 디렉토리 생성
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(path, 0755)
	}
	
	// 랜덤 10자리 문자열 생성 (Java 로직 동일)
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	randomStr := string(b)
	
	filename := filepath.Join(path, randomStr + ".txt")
	
	file, err := os.Create(filename)
	if err != nil {
		fmt.Println("File already exists or error")
	} else {
		file.Close()
		fmt.Printf("File created: %s\n", filename)
	}
	
	return listFiles(path)
}

func listFiles(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	
	var result string
	for _, e := range entries {
		result = e.Name() + " " + result
	}
	return result
}

// Helpers
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getHostname() string {
	host, _ := os.Hostname()
	return host
}

func logInfo(probeType string, status bool) {
	statusStr := "Failed"
	if status { statusStr = "Succeed" }
	fmt.Printf("[Kubernetes] %sProbe is %s -> [System] status: %v\n", probeType, statusStr, status)
}