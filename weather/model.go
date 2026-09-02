package weather

// Unit is the temperature unit the API is asked for. Callers do not convert.
type Unit uint8

const (
	UnitCelsius Unit = iota
	UnitFahrenheit
)

// Query is one Open-Meteo forecast request.
type Query struct {
	Latitude, Longitude float64
	Unit                Unit
	Daily               bool
	Endpoint            string
}

// Current is the newest observation.
type Current struct {
	Temperature float64
	Code        int // WMO weather code
}

// Day is one forecast day.
type Day struct {
	Date            string
	Code            int
	High, Low       float64
	Sunrise, Sunset string
}

// Forecast is a decoded Open-Meteo body. Daily is empty when it was not requested
// or the body carried none.
type Forecast struct {
	Current Current
	Daily   []Day
}
