package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

//go:embed static/index.html
var staticFS embed.FS

type GameState struct {
	Planets   []Planet  `json:"planets"`
	LastSaved time.Time `json:"lastSaved"`
}

type Planet struct {
	Name         string `json:"name"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Faction      string `json:"faction"`
	FactionColor string `json:"factionColor"`
	FactionName  string `json:"factionName"`
	OnFire       bool   `json:"onFire"`
}

type UpdatePlanetRequest struct {
	Name    string  `json:"name"`
	Faction *string `json:"faction,omitempty"`
	OnFire  *bool   `json:"onFire,omitempty"`
}

var (
	gameState = &GameState{Planets: make([]Planet, 0)}
	stateFile = "data/game_state.json"
	mu        sync.RWMutex
)

func main() {
	appDir := getAppDir()
	if err := os.Chdir(appDir); err != nil {
		log.Fatalf("не удалось перейти в папку приложения: %v", err)
	}

	dataDir := filepath.Join(appDir, "data")
	stateFile = filepath.Join(dataDir, "game_state.json")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("не удалось создать папку data: %v", err)
	}

	loadGameState()

	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/api/state", handleState)
	http.HandleFunc("/api/planet", updatePlanet)
	http.HandleFunc("/api/reset", resetGame)

	port := getenv("PORT", "8121")
	url := "http://localhost:" + port

	fmt.Println("========================================")
	fmt.Println("  Alpha Strike Map")
	fmt.Println("  " + url)
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("Данные сохраняются в:", stateFile)
	fmt.Println("Закройте это окно, чтобы остановить сервер.")
	fmt.Println()

	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(url)
	}()

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func getAppDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("Откройте в браузере вручную:", url)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	switch r.Method {
	case http.MethodGet:
		mu.RLock()
		json.NewEncoder(w).Encode(gameState)
		mu.RUnlock()
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func updatePlanet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req UpdatePlanetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range gameState.Planets {
		if gameState.Planets[i].Name != req.Name {
			continue
		}

		if req.Faction != nil {
			gameState.Planets[i].Faction = *req.Faction
			gameState.Planets[i].FactionColor = factionColor(*req.Faction)
			gameState.Planets[i].FactionName = factionName(*req.Faction)
		}
		if req.OnFire != nil {
			gameState.Planets[i].OnFire = *req.OnFire
		}

		gameState.LastSaved = time.Now()
		if err := saveGameStateLocked(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(gameState.Planets[i])
		return
	}

	http.Error(w, "planet not found", http.StatusNotFound)
}

func resetGame(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.Lock()
	initializeGameState()
	mu.Unlock()

	if err := saveGameState(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func loadGameState() {
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		initializeGameState()
		saveGameState()
		return
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		log.Printf("load error: %v", err)
		initializeGameState()
		return
	}

	var loaded GameState
	if err := json.Unmarshal(data, &loaded); err != nil || len(loaded.Planets) == 0 {
		log.Printf("parse error or empty state, reinitializing")
		initializeGameState()
		return
	}

	mu.Lock()
	gameState = &loaded
	mu.Unlock()
	fmt.Printf("loaded %d planets\n", len(gameState.Planets))
}

func saveGameState() error {
	mu.RLock()
	defer mu.RUnlock()
	return saveGameStateLocked()
}

func saveGameStateLocked() error {
	gameState.LastSaved = time.Now()
	data, err := json.MarshalIndent(gameState, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, data, 0644)
}

func initializeGameState() {
	gameState.Planets = buildPlanets()
	gameState.LastSaved = time.Now()
}

func buildPlanets() []Planet {
	raw := []struct {
		Name    string
		X, Y    int
		Faction string
	}{
		{"Campertown", 321, 112, "davion"},
		{"Tsinghai", 335, 71, "davion"},
		{"Chamdo", 412, 76, "davion"},
		{"Lesalles", 406, 143, "davion"},
		{"Raballa", 481, 102, "davion"},
		{"Bora", 580, 116, "davion"},
		{"Old Kentucky", 376, 25, "davion"},
		{"Wazan", 398, 27, "davion"},
		{"Quemoy", 606, 89, "davion"},
		{"Sarmaxa", 657, 134, "davion"},
		{"Sarna", 671, 90, "davion"},
		{"Kaifeng", 768, 100, "davion"},
		{"Truth", 805, 116, "davion"},
		{"Tsingtao", 887, 91, "davion"},
		{"Lee", 1028, 90, "davion"},
		{"Cammal", 1000, 144, "davion"},
		{"Gallitzin", 1090, 130, "davion"},
		{"Monhegan", 1029, 253, "davion"},
		{"Daniels", 1067, 319, "davion"},
		{"Alcyone", 1097, 331, "davion"},
		{"Shoreham", 1073, 447, "davion"},
		{"Weekapaung", 974, 491, "davion"},
		{"Scituate", 856, 459, "davion"},
		{"Kittery", 831, 479, "davion"},
		{"Gurnet", 888, 524, "davion"},
		{"Mentasta", 1016, 559, "davion"},
		{"Beid", 1071, 597, "davion"},
		{"Ziliang", 743, 715, "davion"},
		{"Uravan", 804, 712, "davion"},
		{"Velhas", 750, 790, "davion"},
		{"Immenstadt", 824, 783, "davion"},
		{"Weatogue", 919, 786, "davion"},
		{"Calloway IV", 15, 343, "freeWorlds"},
		{"Les Halles", 120, 298, "freeWorlds"},
		{"Anegasaki", 66, 441, "freeWorlds"},
		{"Shuen Wan", 98, 465, "freeWorlds"},
		{"Ipswich", 81, 535, "freeWorlds"},
		{"Goodna", 182, 575, "freeWorlds"},
		{"Iknogoro", 73, 623, "freeWorlds"},
		{"Cronulla", 183, 640, "freeWorlds"},
		{"Kujari", 201, 720, "freeWorlds"},
		{"Brighton", 810, 347, "stives"},
		{"Nashuar", 911, 356, "stives"},
		{"Armaxa", 958, 377, "stives"},
		{"St. Ives", 949, 427, "stives"},
		{"Taga", 866, 387, "stives"},
		{"Vestallas", 739, 431, "stives"},
		{"Milos", 686, 490, "stives"},
		{"Denbar", 753, 548, "stives"},
		{"Spica", 849, 557, "stives"},
		{"St. Loris", 874, 587, "stives"},
		{"Indicass", 766, 627, "stives"},
		{"Maladar", 977, 620, "stives"},
		{"Tantara", 933, 649, "stives"},
		{"Ambergrist", 831, 677, "stives"},
		{"Texlos", 988, 763, "stives"},
		{"Warlock", 1039, 677, "stives"},
		{"Tallin", 1039, 730, "stives"},
		{"Teng", 1085, 759, "stives"},
		{"Ingersol", 328, 184, "capellan"},
		{"Bandora", 433, 214, "capellan"},
		{"Capella", 573, 183, "capellan"},
		{"No Return", 676, 214, "capellan"},
		{"Randar", 706, 204, "capellan"},
		{"Minnacora", 805, 177, "capellan"},
		{"Ares", 902, 195, "capellan"},
		{"Necromo", 916, 274, "capellan"},
		{"Capricorn III", 850, 267, "capellan"},
		{"New Sagan", 820, 216, "capellan"},
		{"Relevow", 755, 278, "capellan"},
		{"Aldertaine", 605, 294, "capellan"},
		{"Geifer", 568, 266, "capellan"},
		{"Cordiagr", 498, 294, "capellan"},
		{"Masterson", 366, 264, "capellan"},
		{"Propus", 291, 260, "capellan"},
		{"Eom", 241, 297, "capellan"},
		{"Boardwalk", 270, 331, "capellan"},
		{"Kashilla", 346, 342, "capellan"},
		{"Gei-fu", 682, 340, "capellan"},
		{"Jasmine", 172, 358, "capellan"},
		{"Kurragin", 370, 364, "capellan"},
		{"Exedor", 275, 389, "capellan"},
		{"Ovan", 531, 379, "capellan"},
		{"Overton", 616, 391, "capellan"},
		{"Calpaca", 233, 431, "capellan"},
		{"Preston", 427, 425, "capellan"},
		{"Glasgow", 516, 435, "capellan"},
		{"Krin", 306, 467, "capellan"},
		{"Pella II", 175, 501, "capellan"},
		{"Harloc", 597, 486, "capellan"},
		{"Sian", 392, 509, "capellan"},
		{"Bentley", 302, 540, "capellan"},
		{"Hexare", 529, 535, "capellan"},
		{"Imalda", 524, 577, "capellan"},
		{"New Westin", 570, 587, "capellan"},
		{"Frondas", 251, 620, "capellan"},
		{"Fronde", 321, 643, "capellan"},
		{"Castrovia", 429, 648, "capellan"},
		{"Decus", 610, 642, "capellan"},
		{"Hustaing", 667, 609, "capellan"},
		{"Purvo", 693, 684, "capellan"},
		{"Altorra", 333, 710, "capellan"},
		{"Claxton", 440, 715, "capellan"},
		{"Carmen", 514, 715, "capellan"},
		{"Sendalor", 604, 730, "capellan"},
		{"Ito", 394, 780, "capellan"},
		{"Housekarle", 542, 787, "capellan"},
	}

	planets := make([]Planet, 0, len(raw))
	for _, p := range raw {
		planets = append(planets, Planet{
			Name:         p.Name,
			X:            p.X,
			Y:            p.Y,
			Faction:      p.Faction,
			FactionColor: factionColor(p.Faction),
			FactionName:  factionName(p.Faction),
			OnFire:       false,
		})
	}
	return planets
}

func factionColor(faction string) string {
	switch faction {
	case "stives":
		return "#40e0d0"
	case "capellan":
		return "#008000"
	case "davion":
		return "#cd7f32"
	case "freeWorlds":
		return "#800080"
	default:
		return "#888888"
	}
}

func factionName(faction string) string {
	switch faction {
	case "stives":
		return "Сент-Ивское Объединение"
	case "capellan":
		return "Капелланская Конфедерация"
	case "davion":
		return "Дом Дэвион"
	case "freeWorlds":
		return "Содружество Свободных Миров"
	default:
		return "Нейтральная"
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
