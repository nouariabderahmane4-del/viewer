package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// --- 1. DATA STRUCTURES ---

type Specification struct {
	Engine       string `json:"engine"`
	Horsepower   int    `json:"horsepower"`
	Transmission string `json:"transmission"`
	Drivetrain   string `json:"drivetrain"`
}

type Car struct {
	ID             int           `json:"id"`
	Name           string        `json:"name"`
	ManufacturerID int           `json:"manufacturerId"`
	CategoryID     int           `json:"categoryId"`
	Year           int           `json:"year"`
	Specifications Specification `json:"specifications"`
	Image          string        `json:"image"`
}

type Category struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Manufacturer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// PageData: Used for the Home/Search page
type PageData struct {
	Cars  []Car
	Query string
}

// DetailsPageData: Used for the Details page (Car + Recommendations)
type DetailsPageData struct {
	MainCar Car
	Related []Car
}

// --- 2. API CLIENT (The Fetchers) ---

// fetchCars gets all cars
func fetchCars() ([]Car, error) {
	// Using 127.0.0.1 to avoid Windows localhost lookup delays
	resp, err := http.Get("http://127.0.0.1:3000/api/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var cars []Car
	if err := json.NewDecoder(resp.Body).Decode(&cars); err != nil {
		return nil, err
	}
	return cars, nil
}

func fetchCategories() ([]Category, error) {
	resp, err := http.Get("http://127.0.0.1:3000/api/categories")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var categories []Category
	if err := json.NewDecoder(resp.Body).Decode(&categories); err != nil {
		return nil, err
	}
	return categories, nil
}

func fetchManufacturers() ([]Manufacturer, error) {
	resp, err := http.Get("http://127.0.0.1:3000/api/manufacturers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var manufacturers []Manufacturer
	if err := json.NewDecoder(resp.Body).Decode(&manufacturers); err != nil {
		return nil, err
	}
	return manufacturers, nil
}

func fetchCarByID(id int) (*Car, error) {
	url := fmt.Sprintf("http://127.0.0.1:3000/api/models/%d", id)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}

	var car Car
	if err := json.NewDecoder(resp.Body).Decode(&car); err != nil {
		return nil, err
	}
	return &car, nil
}

// --- 3. HTTP HANDLERS (The Logic) ---

// homeHandler: Dashboard + Search + Filtering
func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// A. Fetch Data
	cars, err := fetchCars()
	if err != nil {
		http.Error(w, "Failed to load cars. Is Node running?", http.StatusInternalServerError)
		log.Println("Error fetching cars:", err)
		return
	}
	categories, _ := fetchCategories()
	manufacturers, _ := fetchManufacturers()

	// B. Create Lookup Maps
	catMap := make(map[int]string)
	for _, c := range categories {
		catMap[c.ID] = c.Name
	}
	manMap := make(map[int]string)
	for _, m := range manufacturers {
		manMap[m.ID] = m.Name
	}

	// C. Filter Logic
	query := strings.ToLower(r.URL.Query().Get("q"))
	var filteredCars []Car

	if query != "" {
		for _, car := range cars {
			catName := strings.ToLower(catMap[car.CategoryID])
			manName := strings.ToLower(manMap[car.ManufacturerID])
			carName := strings.ToLower(car.Name)

			// Search Match: Name OR Category OR Brand
			if strings.Contains(carName, query) ||
				strings.Contains(catName, query) ||
				strings.Contains(manName, query) {
				filteredCars = append(filteredCars, car)
			}
		}
	} else {
		filteredCars = cars
	}

	// D. Render
	data := PageData{
		Cars:  filteredCars,
		Query: r.URL.Query().Get("q"),
	}

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Println("Template error:", err)
		return
	}
	tmpl.Execute(w, data)
}

// detailsHandler: Single View + Recommendation Engine
func detailsHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// 1. Fetch Main Car
	mainCar, err := fetchCarByID(id)
	if err != nil {
		http.Error(w, "Car not found", http.StatusNotFound)
		return
	}

	// 2. Fetch All Cars for Recommendations
	allCars, err := fetchCars()
	if err != nil {
		log.Println("Could not fetch cars for recommendations:", err)
	}

	// 3. Recommendation Logic (Same Category)
	var related []Car
	if allCars != nil {
		for _, c := range allCars {
			// Must have same CategoryID, but must NOT be the same car ID
			if c.CategoryID == mainCar.CategoryID && c.ID != mainCar.ID {
				related = append(related, c)
			}
			if len(related) >= 3 { // Limit to 3 items
				break
			}
		}
	}

	// 4. Render
	data := DetailsPageData{
		MainCar: *mainCar,
		Related: related,
	}

	tmpl, err := template.ParseFiles("templates/details.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Println("Template error:", err)
		return
	}
	tmpl.Execute(w, data)
}

// compareHandler: Side-by-Side View
func compareHandler(w http.ResponseWriter, r *http.Request) {
	// Gets all 'id' params: /compare?id=1&id=2
	ids := r.URL.Query()["id"]

	if len(ids) < 2 {
		http.Error(w, "Please select at least 2 cars to compare.", http.StatusBadRequest)
		return
	}

	var selectedCars []Car

	for _, idStr := range ids {
		id, _ := strconv.Atoi(idStr)
		car, err := fetchCarByID(id)
		if err == nil {
			selectedCars = append(selectedCars, *car)
		}
	}

	tmpl, err := template.ParseFiles("templates/compare.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Println("Template error:", err)
		return
	}
	tmpl.Execute(w, selectedCars)
}

// --- 4. MAIN ---
func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/car", detailsHandler)
	http.HandleFunc("/compare", compareHandler)

	fmt.Println("--- CAR VIEWER ULTIMATE ---")
	fmt.Println("Status: Running on http://localhost:8080")
	fmt.Println("Features: Search, Recommendations, Comparison")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
