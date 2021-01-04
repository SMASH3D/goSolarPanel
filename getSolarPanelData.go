package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
)

const panelFile = "panels.json"
const globalFile = "global.json"
const maxPower = 250

/* openuv api token*/
const token = "0e065c55d8005cb5a09ed714f631492b"
const openUvURL = "https://api.openuv.io/api/v1/uv?lat=43.5856664&lng=3.7536762"

/*
The LiveData struct
*/
type LiveData struct {
	CurrentPower int64
	Voltage      int64
	Temperature  int64
	Date         string
}

/*
The Panel struct
*/
type Panel struct {
	InverterID     string
	HistoricalData []LiveData
}

/*
SolarData struct
The solar altitude angle, αs, is the angle between the horizontal and the line to the sun. It is the complement of the zenith angle θz.
The solar azimuth angle, γs, is the angular displacement from south of the projection of beam radiation on the horizontal plane;
displacements east of south are negative and west of south are positive.
*/
type SolarData struct {
	Uvi         float64
	UvMax       float64
	UvMaxTime   string
	SunAltitude float64
	SunAzimuth  float64
}

/*
GlobalData struct
The Global data struct, to represent overall data
*/
type GlobalData struct {
	Date        string
	Power       int64
	Performance float64
	SolarData   SolarData
}

func parseRealTime() map[string]LiveData {
	c := colly.NewCollector()

	re := regexp.MustCompile(`[-]?\d[\d,]*[\.]?[\d{2}]*`)

	dataMap := make(map[string]LiveData)

	c.OnHTML("body > table > tbody > tr", func(e *colly.HTMLElement) {

		liveData := LiveData{}

		InverterID := e.ChildText("td:nth-child(1)")

		if strings.EqualFold(InverterID, "Inverter ID") {
			return
		}
		if power, err := strconv.ParseInt(strings.Join(re.FindAllString(e.ChildText("td:nth-child(2)"), -1), ""), 10, 64); err == nil {
			liveData.CurrentPower = power
		}
		if voltage, err := strconv.ParseInt(strings.Join(re.FindAllString(e.ChildText("td:nth-child(4)"), -1), ""), 10, 64); err == nil {
			liveData.Voltage = voltage
		}
		if temperature, err := strconv.ParseInt(strings.Join(re.FindAllString(e.ChildText("td:nth-child(5)"), -1), ""), 10, 64); err == nil {
			liveData.Temperature = temperature
		}
		liveData.Date = e.ChildText("td:nth-child(6)")

		if liveData.CurrentPower == 0 {
			return
		}
		dataMap[InverterID] = liveData
	})

	c.Visit("http://192.168.1.68/cgi-bin/parameters")
	return dataMap
}

func loadPanelsFromJSON() []Panel {
	panelDataFile, err := os.Open(panelFile)
	if err != nil {
		log.Fatal("opening panels file", err.Error())
	}

	byteValue, _ := ioutil.ReadAll(panelDataFile)

	var panels []Panel

	json.Unmarshal(byteValue, &panels)

	return panels
}

func savePanels(panels []Panel) {
	file, _ := json.MarshalIndent(panels, "", " ")

	_ = ioutil.WriteFile(panelFile, file, 0644)
}

func loadGlobalFromJSON() []GlobalData {
	globalDataFile, err := os.Open(globalFile)
	if err != nil {
		log.Fatal("opening global file", err.Error())
	}

	byteValue, _ := ioutil.ReadAll(globalDataFile)

	var globals []GlobalData

	json.Unmarshal(byteValue, &globals)

	return globals
}

func saveGlobal(globals []GlobalData) {
	file, _ := json.MarshalIndent(globals, "", " ")

	_ = ioutil.WriteFile(globalFile, file, 0644)
}

func getData(url string, token string) map[string]interface{} {

	// Create a new request using http
	req, _ := http.NewRequest("GET", url, nil)

	// add authorization header to the req
	req.Header.Add("x-access-token", token)

	// Send req using http Client
	client := &http.Client{}
	resp, getErr := client.Do(req)
	if getErr != nil {
		log.Println("Error on response.\n[ERRO] -", getErr)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}

	var data map[string]interface{}
	parseErr := json.Unmarshal([]byte(body), &data)
	if parseErr != nil {
		panic(parseErr)
	}
	return data
}

func makeSolarData(data map[string]interface{}) SolarData {
	solarData := SolarData{}

	result := data["result"].(map[string]interface{})

	solarData.Uvi = result["uv"].(float64)
	solarData.UvMax = result["uv_max"].(float64)
	solarData.UvMaxTime = result["uv_max_time"].(string)

	//sunInfo := result["sun_info"].(map[string]interface{})
	sunPosition := result["sun_info"].(map[string]interface{})["sun_position"].(map[string]interface{})

	solarData.SunAltitude = sunPosition["altitude"].(float64)
	solarData.SunAzimuth = sunPosition["azimuth"].(float64)

	return solarData
}

func main() {
	//HANDLING FLAGS
	isVerboseMode := flag.Bool("v", false, "verbose mode")
	flag.Parse()

	//DATA FROM SOLAR PANELS
	liveDataMap := parseRealTime()
	//UVI DATA FROM openweathermap API
	solarData := makeSolarData(getData(openUvURL, token))

	if *isVerboseMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		// Dump json to the standard output
		enc.Encode(liveDataMap)
		enc.Encode(solarData)
	}

	panels := loadPanelsFromJSON()

	globalData := GlobalData{}
	totalOutput := new(int64)
	for i, panel := range panels {
		panels[i].HistoricalData = append(panel.HistoricalData, liveDataMap[panel.InverterID])
		*totalOutput += liveDataMap[panel.InverterID].CurrentPower
	}

	theoreticalTotalOutput := int64(len(panels)) * maxPower

	globalData.Power = *totalOutput
	globalData.Performance = (float64(*totalOutput) / float64(theoreticalTotalOutput) * 100)
	globalData.Date = time.Now().Format("2006-01-02 15:04:05") //Format YYYY-MM-DD hh:mm:ss
	globalData.SolarData = solarData

	globals := loadGlobalFromJSON()

	globals = append(globals, globalData)

	fmt.Println(fmt.Sprintf("%s - %d W - %.2f %% capacity", time.Now().Format("2006-01-02 15:04:05"), *totalOutput, globalData.Performance))

	savePanels(panels)
	saveGlobal(globals)
}

//tarif kWh (12 kVa)
//0.1799 € TTC	0.1496 € TTC
