package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

type TStatZoneConfig struct {
	ZoneNumber      uint8  `json:"zoneNumber,omitempty"`
	CurrentTemp     uint8  `json:"currentTemp"`
	CurrentHumidity uint8  `json:"currentHumidity"`
	TargetHumidity  uint8  `json:"targetHumidity"`
	ZoneName	string `json:"zoneName"`
	FanMode         string `json:"fanMode"`
	Hold            *bool  `json:"hold"`
	HeatSetpoint    uint8  `json:"heatSetpoint"`
	CoolSetpoint    uint8  `json:"coolSetpoint"`
	HoldDuration	string `json:"holdDuration"`
	HoldDurationMins uint16 `json:"holdDurationMins"`
	// the following are global and should be removed from per-zone but are left in for compatibility for now
	OutdoorTemp     uint8  `json:"outdoorTemp"`
	Mode            string `json:"mode"`
	Stage           uint8  `json:"stage"`
	RawMode         uint8  `json:"rawMode"`
}

type TStatZonesConfig struct {
	Zones             []TStatZoneConfig  `json:"zones,omitempty"`
	OutdoorTemp       uint8  `json:"outdoorTemp"`
	Mode              string `json:"mode"`
	Stage             uint8  `json:"stage"`
	RawMode           uint8  `json:"rawMode"`
}

type AirHandler struct {
	BlowerRPM  uint16 `json:"blowerRPM"`
	AirFlowCFM uint16 `json:"airFlowCFM"`
	ElecHeat   bool   `json:"elecHeat"`
}

type HeatPump struct {
	CoilTemp    float32 `json:"coilTemp"`
	OutsideTemp float32 `json:"outsideTemp"`
	Stage       uint8   `json:"stage"`
}

var infinity *InfinityProtocol

func holdTime(ht uint16) string {
	if ht == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%02d", ht/60, ht % 60)
}

// get config and status for all zones in one go
// this is more efficient than getting each zone separately since all the zones' data comes in one pair of serial transactions
func getZonesConfig() (*TStatZonesConfig, bool) {
	cfg := TStatZoneParams{}
	ok := infinity.ReadTable(devTSTAT, &cfg)
	if !ok {
		return nil, false
	}

	params := TStatCurrentParams{}
	ok = infinity.ReadTable(devTSTAT, &params)
	if !ok {
		return nil, false
	}

	tstat := TStatZonesConfig{
		OutdoorTemp:       params.OutdoorAirTemp,
		Mode:              rawModeToString(params.Mode & 0xf),
		Stage:             params.Mode >> 5,
		RawMode:           params.Mode,
	}

	zoneArr := [8]TStatZoneConfig{}

	zc := 0
	for zi := range params.ZCurrentTemp {
		if params.ZCurrentTemp[zi] > 0 && params.ZCurrentTemp[zi] < 255 {
			holdz := ((cfg.ZoneHold & (0x01 << zi)) != 0)

			zoneArr[zc] = TStatZoneConfig{
					ZoneNumber:       uint8(zi+1),
					CurrentTemp:      params.ZCurrentTemp[zi],
					CurrentHumidity:  params.ZCurrentHumidity[zi],
					FanMode:          rawFanModeToString(cfg.ZFanMode[zi]),
					Hold:             &holdz,
					HeatSetpoint:     cfg.ZHeatSetpoint[zi],
					CoolSetpoint:     cfg.ZCoolSetpoint[zi],
					HoldDuration:     holdTime(cfg.ZHoldDuration[zi]),
					HoldDurationMins: cfg.ZHoldDuration[zi],
					ZoneName:         string(cfg.ZName[zi][:]) }

			zc++
		}
	}

	tstat.Zones = zoneArr[0:zc]

	return &tstat, true
}

func getZNConfig(zi int) (*TStatZoneConfig, bool) {
	if (zi < 0 || zi > 7) {
		return nil, false
	}

	cfg := TStatZoneParams{}
	ok := infinity.ReadTable(devTSTAT, &cfg)
	if !ok {
		return nil, false
	}

	params := TStatCurrentParams{}
	ok = infinity.ReadTable(devTSTAT, &params)
	if !ok {
		return nil, false
	}

	hold := cfg.ZoneHold & (0x01 << zi) != 0

	return &TStatZoneConfig{
		CurrentTemp:     params.ZCurrentTemp[zi],
		CurrentHumidity: params.ZCurrentHumidity[zi],
		OutdoorTemp:     params.OutdoorAirTemp,
		Mode:            rawModeToString(params.Mode & 0xf),
		Stage:           params.Mode >> 5,
		FanMode:         rawFanModeToString(cfg.ZFanMode[zi]),
		Hold:            &hold,
		HeatSetpoint:    cfg.ZHeatSetpoint[zi],
		CoolSetpoint:    cfg.ZCoolSetpoint[zi],
		HoldDuration:    holdTime(cfg.ZHoldDuration[zi]),
		HoldDurationMins: cfg.ZHoldDuration[zi],
		ZoneName:        string(cfg.ZName[zi][:]),
		TargetHumidity:  cfg.ZTargetHumidity[zi],
		RawMode:         params.Mode,
	}, true
}

func getTstatSettings() (*TStatSettings, bool) {
	tss := TStatSettings{}
	ok := infinity.ReadTable(devTSTAT, &tss)
	if !ok {
		return nil, false
	}

	return &TStatSettings{
		BacklightSetting: tss.BacklightSetting,
		AutoMode:         tss.AutoMode,
		DeadBand:         tss.DeadBand,
		CyclesPerHour:    tss.CyclesPerHour,
		SchedulePeriods:  tss.SchedulePeriods,
		ProgramsEnabled:  tss.ProgramsEnabled,
		TempUnits:        tss.TempUnits,
		DealerName:       tss.DealerName,
		DealerPhone:      tss.DealerPhone,
	}, true
}

func getAirHandler() (AirHandler, bool) {
	b := cache.get("blower")
	tb, ok := b.(*AirHandler)
	if !ok {
		return AirHandler{}, false
	}
	return *tb, true
}

func getHeatPump() (HeatPump, bool) {
	h := cache.get("heatpump")
	th, ok := h.(*HeatPump)
	if !ok {
		return HeatPump{}, false
	}
	return *th, true
}

func statePoller() {
	for {
		// called once for all zones
		c1, ok := getZonesConfig()
		if ok {
			cache.update("tstat", c1)
		}

		time.Sleep(time.Second * 1)
	}
}

func attachSnoops() {
	// Snoop Heat Pump responses
	infinity.snoopResponse(0x5000, 0x51ff, func(frame *InfinityFrame) {
		data := frame.data[3:]
		heatPump, ok := getHeatPump()
		if ok {
			if bytes.Equal(frame.data[0:3], []byte{0x00, 0x3e, 0x01}) {
				heatPump.CoilTemp = float32(binary.BigEndian.Uint16(data[2:4])) / float32(16)
				heatPump.OutsideTemp = float32(binary.BigEndian.Uint16(data[0:2])) / float32(16)
				log.Debugf("heat pump coil temp is: %f", heatPump.CoilTemp)
				log.Debugf("heat pump outside temp is: %f", heatPump.OutsideTemp)
				cache.update("heatpump", &heatPump)
			} else if bytes.Equal(frame.data[0:3], []byte{0x00, 0x3e, 0x02}) {
				heatPump.Stage = data[0] >> 1
				log.Debugf("HP stage is: %d", heatPump.Stage)
				cache.update("heatpump", &heatPump)
			}
		}
	})

	// Snoop Air Handler responses
	infinity.snoopResponse(0x4000, 0x42ff, func(frame *InfinityFrame) {
		data := frame.data[3:]
		airHandler, ok := getAirHandler()
		if ok {
			if bytes.Equal(frame.data[0:3], []byte{0x00, 0x03, 0x06}) {
				airHandler.BlowerRPM = binary.BigEndian.Uint16(data[1:5])
				log.Debugf("blower RPM is: %d", airHandler.BlowerRPM)
				cache.update("blower", &airHandler)
			} else if bytes.Equal(frame.data[0:3], []byte{0x00, 0x03, 0x16}) {
				airHandler.AirFlowCFM = binary.BigEndian.Uint16(data[4:8])
				airHandler.ElecHeat = data[0]&0x03 != 0
				log.Debugf("air flow CFM is: %d", airHandler.AirFlowCFM)
				cache.update("blower", &airHandler)
			}
		}
	})

	// infinity.snoopResponse(0x6000, 0x62ff, func(frame *InfinityFrame) {
	//	data := frame.data[:]
	//	log.Debug("DamperMsg: ", data)
	// })
}

func main() {
	httpPort := flag.Int("httpport", 8080, "HTTP port to listen on")
	serialPort := flag.String("serial", "", "path to serial port")

	flag.Parse()

	if len(*serialPort) == 0 {
		fmt.Print("must provide serial\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	log.SetLevel(log.DebugLevel)

	infinity = &InfinityProtocol{device: *serialPort}
	airHandler := new(AirHandler)
	heatPump := new(HeatPump)
	cache.update("blower", airHandler)
	cache.update("heatpump", heatPump)
	attachSnoops()
	err := infinity.Open()
	if err != nil {
		log.Panicf("error opening serial port: %s", err.Error())
	}

	go statePoller()
	webserver(*httpPort)
}
