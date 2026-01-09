package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

// Структуры данных
type GameState struct {
	Planets     []Planet            `json:"planets"`
	WarRecords  []WarRecord         `json:"warRecords"`
	Players     []Player            `json:"players"`
	PlayerStats map[int]PlayerStats `json:"playerStats"`
	LastSaved   time.Time           `json:"lastSaved"`
}

type Planet struct {
	Name               string    `json:"name"`
	X                  int       `json:"x"`
	Y                  int       `json:"y"`
	Faction            string    `json:"faction"`
	FactionColor       string    `json:"factionColor"`
	FactionName        string    `json:"factionName"`
	Balance            int       `json:"balance"`
	IsBorderPlanet     bool      `json:"isBorderPlanet"`
	Missions           []Mission `json:"missions"`
	IsNeutral          bool      `json:"isNeutral"`
	OriginalFaction    string    `json:"originalFaction"`
	CurrentFactionName string    `json:"currentFactionName"`
}

type Mission struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type WarRecord struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Planet      string    `json:"planet"`
	UserID      int       `json:"userId"`
	UserName    string    `json:"userName"`
	UserRank    string    `json:"userRank"`
	UserFaction string    `json:"userFaction"`
	Details     string    `json:"details"`
}

type Player struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Rank    string `json:"rank"`
	Faction string `json:"faction"`
}

type PlayerStats struct {
	Victories    int       `json:"victories"`
	Defeats      int       `json:"defeats"`
	Draws        int       `json:"draws"`
	TotalGames   int       `json:"totalGames"`
	LastActivity time.Time `json:"lastActivity"`
	LastGame     LastGame  `json:"lastGame"`
}

type LastGame struct {
	Date    time.Time `json:"date"`
	Planet  string    `json:"planet"`
	Result  string    `json:"result"`
	Points  int       `json:"points"`
	Mission string    `json:"mission"`
}

var (
	gameState = &GameState{
		Planets:     make([]Planet, 0),
		WarRecords:  make([]WarRecord, 0),
		Players:     make([]Player, 0),
		PlayerStats: make(map[int]PlayerStats),
		LastSaved:   time.Now(),
	}
	stateFile = "data/game_state.json"
	mu        sync.RWMutex
)

func main() {
	fmt.Println("🚀 Alpha Strike Server запускается...")
	fmt.Println("🌐 Порт: 8121")
	fmt.Println("📂 Текущая директория:", getCurrentDir())

	// Инициализируем генератор случайных чисел
	rand.Seed(time.Now().UnixNano())

	// Создаем необходимые директории
	createDirs()

	// Загрузка данных
	loadGameState()

	// Настройка маршрутов
	setupRoutes()

	// Автосохранение
	go autoSave()

	// Запуск сервера
	port := getEnv("PORT", "8121")
	fmt.Printf("✅ Сервер запущен: http://localhost:%s\n", port)
	fmt.Println("📁 Статические файлы из: static/")

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func setupRoutes() {
	// Основной маршрут
	http.HandleFunc("/", serveIndex)

	// API маршруты
	http.HandleFunc("/api/state", getState)
	http.HandleFunc("/api/save", saveState)
	http.HandleFunc("/api/complete-mission", completeMission)
	http.HandleFunc("/api/change-faction", changeFaction)
	http.HandleFunc("/api/reset", resetGame)
	http.HandleFunc("/api/export", exportData)
	http.HandleFunc("/api/import", importData)
	http.HandleFunc("/api/add-player", addPlayer)
	http.HandleFunc("/api/remove-player", removePlayer)

	// Статические файлы
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
}

func addPlayer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name    string `json:"name"`
		Rank    string `json:"rank"`
		Faction string `json:"faction"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Генерируем новый ID
	newID := 1
	for _, player := range gameState.Players {
		if player.ID >= newID {
			newID = player.ID + 1
		}
	}

	// Создаем нового игрока
	newPlayer := Player{
		ID:      newID,
		Name:    req.Name,
		Rank:    req.Rank,
		Faction: req.Faction,
	}

	// Добавляем в список
	gameState.Players = append(gameState.Players, newPlayer)

	// Создаем статистику для нового игрока
	gameState.PlayerStats[newID] = PlayerStats{
		Victories:    0,
		Defeats:      0,
		Draws:        0,
		TotalGames:   0,
		LastActivity: time.Now(),
		LastGame:     LastGame{},
	}

	// Записываем в историю
	record := WarRecord{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:   time.Now(),
		Type:        "PLAYER_ADDED",
		Planet:      "Система",
		UserID:      0,
		UserName:    "Система",
		UserRank:    "Администратор",
		UserFaction: "system",
		Details: fmt.Sprintf("Добавлен новый игрок: %s (%s), фракция: %s",
			req.Name, req.Rank, getFactionName(req.Faction)),
	}

	gameState.WarRecords = append(gameState.WarRecords, record)
	gameState.LastSaved = time.Now()

	// Сохраняем изменения
	saveGameState()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"player":  newPlayer,
		"message": "Игрок успешно добавлен",
	})
}

func removePlayer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PlayerID int `json:"playerId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Ищем игрока
	playerIndex := -1
	var playerName string
	for i, player := range gameState.Players {
		if player.ID == req.PlayerID {
			playerIndex = i
			playerName = player.Name
			break
		}
	}

	if playerIndex == -1 {
		http.Error(w, "Игрок не найден", http.StatusNotFound)
		return
	}

	// Удаляем игрока из списка
	gameState.Players = append(gameState.Players[:playerIndex], gameState.Players[playerIndex+1:]...)

	// Удаляем статистику игрока
	delete(gameState.PlayerStats, req.PlayerID)

	// Записываем в историю
	record := WarRecord{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:   time.Now(),
		Type:        "PLAYER_REMOVED",
		Planet:      "Система",
		UserID:      0,
		UserName:    "Система",
		UserRank:    "Администратор",
		UserFaction: "system",
		Details:     fmt.Sprintf("Удален игрок: %s (ID: %d)", playerName, req.PlayerID),
	}

	gameState.WarRecords = append(gameState.WarRecords, record)
	gameState.LastSaved = time.Now()

	// Сохраняем изменения
	saveGameState()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Игрок %s успешно удален", playerName),
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/index.html")
}

func getState(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	json.NewEncoder(w).Encode(gameState)
}

func saveState(w http.ResponseWriter, r *http.Request) {
	if err := saveGameState(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "Данные сохранены",
		"lastSaved": gameState.LastSaved,
	})
}

func completeMission(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PlanetName   string `json:"planetName"`
		MissionIndex int    `json:"missionIndex"`
		Participants []struct {
			ID      int    `json:"id"`
			Faction string `json:"faction"`
		} `json:"participants"`
		Points         int    `json:"points"`
		WinningFaction string `json:"winningFaction"`
		Description    string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Находим планету
	planetIndex := -1
	for i, p := range gameState.Planets {
		if p.Name == req.PlanetName {
			planetIndex = i
			break
		}
	}

	if planetIndex == -1 {
		http.Error(w, "Планета не найдена", http.StatusNotFound)
		return
	}

	planet := &gameState.Planets[planetIndex]

	// Проверяем миссию
	if len(planet.Missions) <= req.MissionIndex {
		http.Error(w, "Миссия не найдена", http.StatusNotFound)
		return
	}

	// Обновляем баланс
	attackerFaction := getAttackerFaction(planet.Faction)
	if req.WinningFaction == planet.Faction {
		// Защитники выиграли
		planet.Balance = max(0, planet.Balance-req.Points)
	} else if req.WinningFaction == attackerFaction {
		// Атакующие выиграли
		planet.Balance = min(500, planet.Balance+req.Points)

		// Проверяем захват
		if planet.Balance >= 500 {
			planet.Faction = attackerFaction
			planet.Balance = 0
			planet.FactionColor = getFactionColor(attackerFaction)
			planet.FactionName = getFactionName(attackerFaction)
			planet.CurrentFactionName = getFactionName(attackerFaction)
			planet.IsNeutral = false
			planet.Missions = generateRandomMissions(3)
		}
	}

	// Удаляем выполненную миссию
	completedMission := planet.Missions[req.MissionIndex]
	planet.Missions = append(planet.Missions[:req.MissionIndex],
		planet.Missions[req.MissionIndex+1:]...)

	// Добавляем новую случайную миссию если есть место
	if planet.IsBorderPlanet && len(planet.Missions) < 3 {
		// Получаем список уже существующих миссий
		existingMissions := make(map[string]bool)
		for _, m := range planet.Missions {
			existingMissions[m.Name] = true
		}

		// Генерируем новую уникальную миссию
		newMission := getRandomMission()
		// Проверяем, что миссия не повторяется (максимум 5 попыток)
		attempts := 0
		for existingMissions[newMission] && attempts < 5 {
			newMission = getRandomMission()
			attempts++
		}

		planet.Missions = append(planet.Missions, Mission{
			Name:   newMission,
			Status: "active",
		})
	}

	// Обновляем статистику игроков
	for _, participant := range req.Participants {
		stats := gameState.PlayerStats[participant.ID]
		stats.TotalGames++
		stats.LastActivity = time.Now()

		result := "DEFEAT"
		if participant.Faction == req.WinningFaction {
			stats.Victories++
			result = "VICTORY"
		} else {
			stats.Defeats++
		}

		stats.LastGame = LastGame{
			Date:    time.Now(),
			Planet:  req.PlanetName,
			Result:  result,
			Points:  req.Points,
			Mission: completedMission.Name,
		}

		gameState.PlayerStats[participant.ID] = stats
	}

	// Записываем в историю
	record := WarRecord{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:   time.Now(),
		Type:        "MISSION_COMPLETE",
		Planet:      req.PlanetName,
		UserID:      req.Participants[0].ID,
		UserName:    "Игрок " + fmt.Sprint(req.Participants[0].ID),
		UserRank:    "Участник",
		UserFaction: req.Participants[0].Faction,
		Details:     fmt.Sprintf("Миссия: %s, Очки: %d, Победитель: %s, %s", completedMission.Name, req.Points, req.WinningFaction, req.Description),
	}

	gameState.WarRecords = append(gameState.WarRecords, record)
	gameState.LastSaved = time.Now()

	// Сохраняем состояние
	saveGameState()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"planet":  planet,
		"record":  record,
	})
}

func changeFaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PlanetName string `json:"planetName"`
		NewFaction string `json:"newFaction"`
		Reason     string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for i := range gameState.Planets {
		if gameState.Planets[i].Name == req.PlanetName {
			oldFaction := gameState.Planets[i].Faction

			gameState.Planets[i].Faction = req.NewFaction
			gameState.Planets[i].FactionColor = getFactionColor(req.NewFaction)
			gameState.Planets[i].FactionName = getFactionName(req.NewFaction)
			gameState.Planets[i].CurrentFactionName = getFactionName(req.NewFaction)
			gameState.Planets[i].Balance = 0
			gameState.Planets[i].IsNeutral = !(req.NewFaction == "stives" || req.NewFaction == "capellan")
			gameState.Planets[i].OriginalFaction = oldFaction

			if req.NewFaction == "stives" || req.NewFaction == "capellan" {
				gameState.Planets[i].Missions = generateRandomMissions(3)
				gameState.Planets[i].IsBorderPlanet = true
			} else {
				gameState.Planets[i].Missions = []Mission{}
				gameState.Planets[i].IsBorderPlanet = false
			}

			// Записываем в историю
			record := WarRecord{
				ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
				Timestamp:   time.Now(),
				Type:        "CHANGE_FACTION",
				Planet:      req.PlanetName,
				UserID:      0,
				UserName:    "Система",
				UserRank:    "Администратор",
				UserFaction: req.NewFaction,
				Details: fmt.Sprintf("Фракция изменена: %s → %s. Причина: %s",
					getFactionName(oldFaction), getFactionName(req.NewFaction), req.Reason),
			}

			gameState.WarRecords = append(gameState.WarRecords, record)
			gameState.LastSaved = time.Now()

			// Сохраняем состояние
			saveGameState()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"planet":  gameState.Planets[i],
				"record":  record,
			})
			return
		}
	}

	http.Error(w, "Планета не найдена", http.StatusNotFound)
}

func resetGame(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	initializeGameState()
	mu.Unlock()

	saveGameState()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Игра сброшена к начальному состоянию",
	})
}

func exportData(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=alpha_strike_export.json")

	json.NewEncoder(w).Encode(gameState)
}

func importData(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var newState GameState
	if err := json.NewDecoder(r.Body).Decode(&newState); err != nil {
		http.Error(w, "Неверный формат данных", http.StatusBadRequest)
		return
	}

	mu.Lock()
	gameState = &newState
	gameState.LastSaved = time.Now()
	mu.Unlock()

	saveGameState()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Данные успешно импортированы",
	})
}

// Вспомогательные функции
func createDirs() {
	os.MkdirAll("data", 0755)
	os.MkdirAll("static", 0755)
}

func getCurrentDir() string {
	dir, _ := os.Getwd()
	return dir
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func loadGameState() {
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		fmt.Println("📂 Создаем начальное состояние игры...")
		initializeGameState()
		saveGameState()
		return
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		log.Printf("❌ Ошибка загрузки данных: %v", err)
		initializeGameState()
		return
	}

	if err := json.Unmarshal(data, &gameState); err != nil {
		log.Printf("❌ Ошибка парсинга данных: %v", err)
		initializeGameState()
		return
	}

	fmt.Printf("✅ Загружено: %d планет, %d записей, %d игроков\n",
		len(gameState.Planets), len(gameState.WarRecords), len(gameState.Players))
}

func saveGameState() error {
	mu.RLock()
	defer mu.RUnlock()

	gameState.LastSaved = time.Now()

	data, err := json.MarshalIndent(gameState, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(stateFile, data, 0644)
}

func autoSave() {
	for {
		time.Sleep(5 * time.Minute)
		if err := saveGameState(); err != nil {
			log.Printf("❌ Ошибка автосохранения: %v", err)
		} else {
			fmt.Printf("💾 Данные сохранены (%s)\n", time.Now().Format("15:04:05"))
		}
	}
}

// Инициализация планет из frontend данных
func initializePlanetsFromFrontend() []Planet {
	// Точные данные из frontend index.html (массив planets)
	frontendPlanets := []struct {
		Name    string
		X       int
		Y       int
		Faction string
	}{
		// Дом Дэвион (бронзовые)
		{Name: "Campertown", X: 321, Y: 112, Faction: "davion"},
		{Name: "Tsinghai", X: 335, Y: 71, Faction: "davion"},
		{Name: "Chamdo", X: 412, Y: 76, Faction: "davion"},
		{Name: "Lesalles", X: 406, Y: 143, Faction: "davion"},
		{Name: "Raballa", X: 481, Y: 102, Faction: "davion"},
		{Name: "Bora", X: 580, Y: 116, Faction: "davion"},
		{Name: "Old Kentucky", X: 376, Y: 25, Faction: "davion"},
		{Name: "Wazan", X: 398, Y: 27, Faction: "davion"},
		{Name: "Quemoy", X: 606, Y: 89, Faction: "davion"},
		{Name: "Sarmaxa", X: 657, Y: 134, Faction: "davion"},
		{Name: "Sarna", X: 671, Y: 90, Faction: "davion"},
		{Name: "Kaifeng", X: 768, Y: 100, Faction: "davion"},
		{Name: "Truth", X: 805, Y: 116, Faction: "davion"},
		{Name: "Tsingtao", X: 887, Y: 91, Faction: "davion"},
		{Name: "Lee", X: 1028, Y: 90, Faction: "davion"},
		{Name: "Cammal", X: 1000, Y: 144, Faction: "davion"},
		{Name: "Gallitzin", X: 1090, Y: 130, Faction: "davion"},
		{Name: "Monhegan", X: 1029, Y: 253, Faction: "davion"},
		{Name: "Daniels", X: 1067, Y: 319, Faction: "davion"},
		{Name: "Alcyone", X: 1097, Y: 331, Faction: "davion"},
		{Name: "Shoreham", X: 1073, Y: 447, Faction: "davion"},
		{Name: "Weekapaung", X: 974, Y: 491, Faction: "davion"},
		{Name: "Scituate", X: 856, Y: 459, Faction: "davion"},
		{Name: "Kittery", X: 831, Y: 479, Faction: "davion"},
		{Name: "Gurnet", X: 888, Y: 524, Faction: "davion"},
		{Name: "Mentasta", X: 1016, Y: 559, Faction: "davion"},
		{Name: "Beid", X: 1071, Y: 597, Faction: "davion"},
		{Name: "Ziliang", X: 743, Y: 715, Faction: "davion"},
		{Name: "Uravan", X: 804, Y: 712, Faction: "davion"},
		{Name: "Velhas", X: 750, Y: 790, Faction: "davion"},
		{Name: "Immenstadt", X: 824, Y: 783, Faction: "davion"},
		{Name: "Weatogue", X: 919, Y: 786, Faction: "davion"},

		// Содружество Свободных Миров (фиолетовые)
		{Name: "Calloway IV", X: 15, Y: 343, Faction: "freeWorlds"},
		{Name: "Les Halles", X: 120, Y: 298, Faction: "freeWorlds"},
		{Name: "Anegasaki", X: 66, Y: 441, Faction: "freeWorlds"},
		{Name: "Shuen Wan", X: 98, Y: 465, Faction: "freeWorlds"},
		{Name: "Ipswich", X: 81, Y: 535, Faction: "freeWorlds"},
		{Name: "Goodna", X: 182, Y: 575, Faction: "freeWorlds"},
		{Name: "Iknogoro", X: 73, Y: 623, Faction: "freeWorlds"},
		{Name: "Cronulla", X: 183, Y: 640, Faction: "freeWorlds"},
		{Name: "Kujari", X: 201, Y: 720, Faction: "freeWorlds"},

		// Сент-Ивское Объединение (бирюзовые)
		{Name: "Brighton", X: 810, Y: 347, Faction: "stives"},
		{Name: "Nashuar", X: 911, Y: 356, Faction: "stives"},
		{Name: "Armaxa", X: 958, Y: 377, Faction: "stives"},
		{Name: "St. Ives", X: 949, Y: 427, Faction: "stives"},
		{Name: "Taga", X: 866, Y: 387, Faction: "stives"},
		{Name: "Vestallas", X: 739, Y: 431, Faction: "stives"},
		{Name: "Milos", X: 686, Y: 490, Faction: "stives"},
		{Name: "Denbar", X: 753, Y: 548, Faction: "stives"},
		{Name: "Spica", X: 849, Y: 557, Faction: "stives"},
		{Name: "St. Loris", X: 874, Y: 587, Faction: "stives"},
		{Name: "Indicass", X: 766, Y: 627, Faction: "stives"},
		{Name: "Maladar", X: 977, Y: 620, Faction: "stives"},
		{Name: "Tantara", X: 933, Y: 649, Faction: "stives"},
		{Name: "Ambergrist", X: 831, Y: 677, Faction: "stives"},
		{Name: "Texlos", X: 988, Y: 763, Faction: "stives"},
		{Name: "Warlock", X: 1039, Y: 677, Faction: "stives"},
		{Name: "Tallin", X: 1039, Y: 730, Faction: "stives"},
		{Name: "Teng", X: 1085, Y: 759, Faction: "stives"},

		// Капелланская Конфедерация (зеленые)
		{Name: "Ingersol", X: 328, Y: 184, Faction: "capellan"},
		{Name: "Bandora", X: 433, Y: 214, Faction: "capellan"},
		{Name: "Capella", X: 573, Y: 183, Faction: "capellan"},
		{Name: "No Return", X: 676, Y: 214, Faction: "capellan"},
		{Name: "Randar", X: 706, Y: 204, Faction: "capellan"},
		{Name: "Minnacora", X: 805, Y: 177, Faction: "capellan"},
		{Name: "Ares", X: 902, Y: 195, Faction: "capellan"},
		{Name: "Necromo", X: 916, Y: 274, Faction: "capellan"},
		{Name: "Capricorn III", X: 850, Y: 267, Faction: "capellan"},
		{Name: "New Sagan", X: 820, Y: 216, Faction: "capellan"},
		{Name: "Relevow", X: 755, Y: 278, Faction: "capellan"},
		{Name: "Aldertaine", X: 605, Y: 294, Faction: "capellan"},
		{Name: "Geifer", X: 568, Y: 266, Faction: "capellan"},
		{Name: "Cordiagr", X: 498, Y: 294, Faction: "capellan"},
		{Name: "Masterson", X: 366, Y: 264, Faction: "capellan"},
		{Name: "Propus", X: 291, Y: 260, Faction: "capellan"},
		{Name: "Eom", X: 241, Y: 297, Faction: "capellan"},
		{Name: "Boardwalk", X: 270, Y: 331, Faction: "capellan"},
		{Name: "Kashilla", X: 346, Y: 342, Faction: "capellan"},
		{Name: "Gei-fu", X: 682, Y: 340, Faction: "capellan"},
		{Name: "Jasmine", X: 172, Y: 358, Faction: "capellan"},
		{Name: "Kurragin", X: 370, Y: 364, Faction: "capellan"},
		{Name: "Exedor", X: 275, Y: 389, Faction: "capellan"},
		{Name: "Ovan", X: 531, Y: 379, Faction: "capellan"},
		{Name: "Overton", X: 616, Y: 391, Faction: "capellan"},
		{Name: "Calpaca", X: 233, Y: 431, Faction: "capellan"},
		{Name: "Preston", X: 427, Y: 425, Faction: "capellan"},
		{Name: "Glasgow", X: 516, Y: 435, Faction: "capellan"},
		{Name: "Krin", X: 306, Y: 467, Faction: "capellan"},
		{Name: "Pella II", X: 175, Y: 501, Faction: "capellan"},
		{Name: "Harloc", X: 597, Y: 486, Faction: "capellan"},
		{Name: "Sian", X: 392, Y: 509, Faction: "capellan"},
		{Name: "Bentley", X: 302, Y: 540, Faction: "capellan"},
		{Name: "Hexare", X: 529, Y: 535, Faction: "capellan"},
		{Name: "Imalda", X: 524, Y: 577, Faction: "capellan"},
		{Name: "New Westin", X: 570, Y: 587, Faction: "capellan"},
		{Name: "Frondas", X: 251, Y: 620, Faction: "capellan"},
		{Name: "Fronde", X: 321, Y: 643, Faction: "capellan"},
		{Name: "Castrovia", X: 429, Y: 648, Faction: "capellan"},
		{Name: "Decus", X: 610, Y: 642, Faction: "capellan"},
		{Name: "Hustaing", X: 667, Y: 609, Faction: "capellan"},
		{Name: "Purvo", X: 693, Y: 684, Faction: "capellan"},
		{Name: "Altorra", X: 333, Y: 710, Faction: "capellan"},
		{Name: "Claxton", X: 440, Y: 715, Faction: "capellan"},
		{Name: "Carmen", X: 514, Y: 715, Faction: "capellan"},
		{Name: "Sendalor", X: 604, Y: 730, Faction: "capellan"},
		{Name: "Ito", X: 394, Y: 780, Faction: "capellan"},
		{Name: "Housekarle", X: 542, Y: 787, Faction: "capellan"},
	}

	planets := make([]Planet, 0, len(frontendPlanets))

	for _, fp := range frontendPlanets {
		isBorderPlanet := calculateIsBorderPlanet(fp, frontendPlanets)

		planet := Planet{
			Name:               fp.Name,
			X:                  fp.X,
			Y:                  fp.Y,
			Faction:            fp.Faction,
			FactionColor:       getFactionColor(fp.Faction),
			FactionName:        getFactionName(fp.Faction),
			Balance:            0,
			IsBorderPlanet:     isBorderPlanet,
			Missions:           []Mission{},
			IsNeutral:          !(fp.Faction == "stives" || fp.Faction == "capellan"),
			OriginalFaction:    fp.Faction,
			CurrentFactionName: getFactionName(fp.Faction),
		}

		// Генерируем СЛУЧАЙНЫЕ миссии для пограничных планет St.Ives и Capellan
		if isBorderPlanet && (fp.Faction == "stives" || fp.Faction == "capellan") {
			planet.Missions = generateRandomMissions(3)
		}

		planets = append(planets, planet)
	}

	return planets
}

// Вычисляем, является ли планета пограничной
func calculateIsBorderPlanet(planet struct {
	Name    string
	X       int
	Y       int
	Faction string
}, allPlanets []struct {
	Name    string
	X       int
	Y       int
	Faction string
}) bool {
	// Планета считается пограничной, если она принадлежит St.Ives или Capellan
	// и находится рядом с планетой вражеской фракции
	if planet.Faction != "stives" && planet.Faction != "capellan" {
		return false
	}

	enemyFaction := "stives"
	if planet.Faction == "stives" {
		enemyFaction = "capellan"
	}

	const borderDistance = 150

	for _, otherPlanet := range allPlanets {
		if otherPlanet.Faction != enemyFaction {
			continue
		}

		dx := planet.X - otherPlanet.X
		dy := planet.Y - otherPlanet.Y
		distance := math.Sqrt(float64(dx*dx + dy*dy))

		if distance <= borderDistance {
			return true
		}
	}

	return false
}

func initializeGameState() {
	// Инициализация планет из frontend данных
	gameState.Planets = initializePlanetsFromFrontend()

	// Игроки
	gameState.Players = []Player{
		{ID: 1, Name: "Мишкин Артём", Rank: "Командир Нова", Faction: "stives"},
		{ID: 2, Name: "Андрей Павлов", Rank: "Сао-Вей", Faction: "capellan"},
		{ID: 3, Name: "Жеребцов Михаил (Микка)", Rank: "Рядовой", Faction: "capellan"},
		{ID: 4, Name: "Макушкин Александр", Rank: "Шиа-Бен-Бинг", Faction: "capellan"},
		{ID: 5, Name: "Boris Leonov", Rank: "Командир Нова", Faction: "stives"},
		{ID: 6, Name: "Олег Пачев", Rank: "Звёздный Командир", Faction: "stives"},
		{ID: 7, Name: "Архан", Rank: "Адепт", Faction: "stives"},
		{ID: 8, Name: "Сергей Протасов", Rank: "Сао-Вей", Faction: "capellan"},
		{ID: 9, Name: "Леонид Черкасов", Rank: "Мехвоин", Faction: "stives"},
		{ID: 10, Name: "Amardil", Rank: "Рядовой", Faction: "stives"},
		{ID: 11, Name: "Tihiron Rrr (Данила)", Rank: "Рядовой", Faction: "capellan"},
		{ID: 12, Name: "Шестериков Александр", Rank: "Мехвоин", Faction: "stives"},
		{ID: 13, Name: "Шестерикова Таисия", Rank: "Шиа-Бен-Бинг", Faction: "capellan"},
		{ID: 14, Name: "Андрей Шумов", Rank: "Сержант", Faction: "stives"},
	}

	// Инициализация статистики
	gameState.PlayerStats = make(map[int]PlayerStats)
	for _, player := range gameState.Players {
		gameState.PlayerStats[player.ID] = PlayerStats{
			Victories:    0,
			Defeats:      0,
			Draws:        0,
			TotalGames:   0,
			LastActivity: time.Now(),
			LastGame:     LastGame{},
		}
	}

	// Начальная запись
	gameState.WarRecords = []WarRecord{{
		ID:          "init",
		Timestamp:   time.Now(),
		Type:        "SYSTEM",
		Planet:      "Вселенная",
		UserID:      0,
		UserName:    "Система",
		UserRank:    "Администратор",
		UserFaction: "system",
		Details:     fmt.Sprintf("Система Alpha Strike инициализирована. Планет: %d", len(gameState.Planets)),
	}}

	gameState.LastSaved = time.Now()
}

// Генерация случайных уникальных миссий
func generateRandomMissions(count int) []Mission {
	missionsList := []string{
		"Эвакуация груза",
		"Контроль поля битвы",
		"Уничтожение противника",
		"Удержание позиции",
		"Прорыв",
		"Поиск и уничтожение",
		"Захват сброшенного груза",
		"Эскорт",
		"Удержание боевых точек",
	}

	// Если нужно больше миссий, чем есть в списке, возвращаем все
	if count >= len(missionsList) {
		result := make([]Mission, len(missionsList))
		for i, name := range missionsList {
			result[i] = Mission{
				Name:   name,
				Status: "active",
			}
		}
		return result
	}

	// Перемешиваем миссии
	shuffled := make([]string, len(missionsList))
	copy(shuffled, missionsList)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Берем первые count миссий
	result := make([]Mission, 0, count)
	for i := 0; i < count; i++ {
		result = append(result, Mission{
			Name:   shuffled[i],
			Status: "active",
		})
	}

	return result
}

// Получение одной случайной миссии
func getRandomMission() string {
	missions := []string{
		"Эвакуация груза",
		"Контроль поля битвы",
		"Уничтожение противника",
		"Удержание позиции",
		"Прорыв",
		"Поиск и уничтожение",
		"Захват сброшенного груза",
		"Эскорт",
		"Удержание боевых точек",
	}
	return missions[rand.Intn(len(missions))]
}

// Старая функция для совместимости
func generateMissions(count int) []Mission {
	return generateRandomMissions(count)
}

func getFactionColor(faction string) string {
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

func getFactionName(faction string) string {
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
		return "Нейтральная фракция"
	}
}

func getAttackerFaction(defenderFaction string) string {
	if defenderFaction == "stives" {
		return "capellan"
	} else if defenderFaction == "capellan" {
		return "stives"
	}
	return "neutral"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
