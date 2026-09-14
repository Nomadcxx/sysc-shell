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

// Current is the newest observation. The optional fields are nil when the
// body did not carry them, and the UI renders a dash rather than a silent
// zero; only Temperature and Code are required of every response.
type Current struct {
	Temperature   float64
	Code          int // WMO weather code
	Apparent      *float64
	IsDay         *bool
	Humidity      *float64
	WindSpeed     *float64
	WindDirection *float64
	UVIndex       *float64
}

// Day is one forecast day. The optional fields are nil when the body did not
// carry them, for the same reason Current's are.
type Day struct {
	Date                     string
	Code                     int
	High, Low                float64
	Sunrise, Sunset          string
	UVIndexMax               *float64
	PrecipitationProbability *float64
	Precipitation            *float64
}

// Forecast is a decoded Open-Meteo body. Daily is empty when it was not requested
// or the body carried none. Elevation and the timezone names come from the
// response root; both are optional.
type Forecast struct {
	Current              Current
	Daily                []Day
	Elevation            *float64
	Timezone             string
	TimezoneAbbreviation string
}
