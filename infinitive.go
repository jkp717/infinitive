package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"encoding/hex"
	"os"
	"strconv"
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
	Preset          string `json:"preset"`
	HeatSetpoint    uint8  `json:"heatSetpoint"`
	CoolSetpoint    uint8  `json:"coolSetpoint"`
	OvrdDuration	string `json:"overrideDuration"`
	OvrdDurationMins uint16 `json:"overrideDurationMins"`
	// the following are global and should be removed from per-zone but are left in for compatibility for now
	OutdoorTemp     uint8  `json:"outdoorTemp"`
	Mode            string `json:"mode"`
	Stage           uint8  `json:"stage"`
	Action          string `json:"action"`
	RawMode         uint8  `json:"rawMode"`
}

type TStatZonesConfig struct {
	Zones             []TStatZoneConfig  `json:"zones,omitempty"`
	OutdoorTemp       uint8  `json:"outdoorTemp"`
	Mode              string `json:"mode"`
	Stage             uint8  `json:"stage"`
	Action            string `json:"action"`
	RawMode           uint8  `json:"rawMode"`
}

type AirHandler struct {
	BlowerRPM      uint16  `json:"blowerRPM"`
	AirFlowCFM     uint16  `json:"airFlowCFM"`
	StaticPressure float32 `json:"staticPressure"`
	HeatStage      uint8   `json:"heatStage"`
	ElecHeat       bool    `json:"elecHeat"`
	Action         string  `json:"action"`
}

type HeatPump struct {
	CoilTemp    float32 `json:"coilTemp"`
	OutsideTemp float32 `json:"outsideTemp"`
	Stage       uint8   `json:"stage"`
}

type DamperPosition struct {
	DamperPos   [8]uint8 `json:"damperPosition"`
}

type Logger struct {
	f	*os.File
	basems int64
	tds	string
}

var RLogger Logger;

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
		Action:            rawActionToString(params.Mode >> 5),
		RawMode:           params.Mode,
	}

	zoneArr := [8]TStatZoneConfig{}

	zc := 0
	for zi := range params.ZCurrentTemp {
		if params.ZCurrentTemp[zi] > 0 && params.ZCurrentTemp[zi] < 255 {
			holdz := ((cfg.ZoneHold & (0x01 << zi)) != 0)
			presetz := "none"

			if holdz {
				presetz = "hold"
			}

			zoneArr[zc] = TStatZoneConfig{
					ZoneNumber:       uint8(zi+1),
					CurrentTemp:      params.ZCurrentTemp[zi],
					CurrentHumidity:  params.ZCurrentHumidity[zi],
					FanMode:          rawFanModeToString(cfg.ZFanMode[zi]),
					Hold:             &holdz,
					Preset:           presetz,
					HeatSetpoint:     cfg.ZHeatSetpoint[zi],
					CoolSetpoint:     cfg.ZCoolSetpoint[zi],
					OvrdDuration:     holdTime(cfg.ZOvrdDuration[zi]),
					OvrdDurationMins: cfg.ZOvrdDuration[zi],
					ZoneName:         string(bytes.Trim(cfg.ZName[zi][:], " \000")) }

			zc++
		}
	}

	tstat.Zones = zoneArr[0:zc]

	return &tstat, true
}


// write a change to a single parameter of a single zone or global config
// zn == 0 for global params or 1-8 for zone params
// returns ok == true
func putConfig(zone string, param string, value string) bool {
	params := TStatZoneParams{}
	flags := byte(0)

	zn, err := strconv.Atoi(zone)
	if err != nil {
		log.Errorf("putConfig: invalid zone value '%s'", zone)
		return false
	}
	zi := zn - 1

	// zone parameters
	if (zn >= 1 && zn <= 8) {
		switch param {
		case "fanMode":
			if mode, ok := stringFanModeToRaw(value); !ok {
				log.Errorf("putConfig: invalid fan mode name '%s' for zone %d", value, zn)
				return false
			} else {
				params.ZFanMode[zi] = mode
				flags |= 0x01
			}
		case "coolSetpoint":
			if val, err := strconv.ParseUint(value, 10, 8); err != nil {
				log.Errorf("putConfig: invalid cool setpoint value '%s' for zone %d", value, zn)
				return false
			} else {
				params.ZCoolSetpoint[zi] = uint8(val)
				flags |= 0x08
			}
		case "heatSetpoint":
			if val, err := strconv.ParseUint(value, 10, 8); err != nil {
				log.Errorf("putConfig: invalid heat setpoint value '%s' for zone %d", value, zn)
				return false
			} else {
				params.ZHeatSetpoint[zi] = uint8(val)
				flags |= 0x04
			}
		case "hold":	// dedicated 'hold' semantics
			var val bool
			switch value {
				case "true":
					val = true
				case "false":
					val = false
				default:
					log.Errorf("putConfig: invalid hold value '%s' for zone %d", value, zn)
					return false
				}
			if val {
				params.ZoneHold = 0x01 << zi
			}
			flags |= 0x02
		case "preset":	// 'preset' semantics to control hold - extend this if we add more presets
			var val bool
			switch value {
				case "hold":
					val = true
				case "none":
					val = false
				default:
					log.Errorf("putConfig: invalid preset value '%s' for zone %d", value, zn)
					return false
				}
			if val {
				params.ZoneHold = 0x01 << zi
			}
			flags |= 0x02
		default:
			log.Errorf("putConfig: invalid parameter name '%s' for zone %d", param, zn)
			return false
		}

		if flags != 0 {
			log.Infof("calling WriteTableZ with flags: %d, 0x%x", zi, flags)
			infinity.WriteTableZ(devTSTAT, params, uint8(zi), flags)
		}

		return true
	} else if zn == 0 {
		switch param {
		case "mode":
			if mode, ok := stringModeToRaw(value); !ok {
				log.Errorf("putConfig: invalid mode value '%s'", value)
				return false
			} else {
				p := TStatCurrentParams{Mode: mode}
				infinity.WriteTable(devTSTAT, p, 0x10)
				return true
			}
		default:
			log.Errorf("putConfig: invalid parameter name '%s'", param)
			return false
		}
	}

	log.Errorf("putConfig: invalid zone number %d", zn)
	return false
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
	presetz := "none"

	if hold {
		presetz = "hold"
	}

	return &TStatZoneConfig{
		CurrentTemp:     params.ZCurrentTemp[zi],
		CurrentHumidity: params.ZCurrentHumidity[zi],
		OutdoorTemp:     params.OutdoorAirTemp,
		Mode:            rawModeToString(params.Mode & 0xf),
		Stage:           params.Mode >> 5,
		Action:          rawActionToString(params.Mode >> 5),
		FanMode:         rawFanModeToString(cfg.ZFanMode[zi]),
		Hold:            &hold,
		Preset:          presetz,
		HeatSetpoint:    cfg.ZHeatSetpoint[zi],
		CoolSetpoint:    cfg.ZCoolSetpoint[zi],
		OvrdDuration:    holdTime(cfg.ZOvrdDuration[zi]),
		OvrdDurationMins: cfg.ZOvrdDuration[zi],
		ZoneName:        string(bytes.Trim(cfg.ZName[zi][:], " \000")),
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

func getRawData(dev uint16, tbl []byte) {
	var addr InfinityTableAddr
	copy(addr[:], tbl[0:3])
	raw := InfinityProtocolRawRequest{&[]byte{}}

	success := infinity.Read(dev, addr, raw)

	if success {
		log.Debugf("RAW: %04x/%02x%02x%02x: %s", dev, tbl[0], tbl[1], tbl[2], hex.EncodeToString(*raw.data))
	} else {
		log.Debugf("RAW: %04x/%02x%02x%02x: timeout", dev, tbl[0], tbl[1], tbl[2])
	}
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

func getDamperPosition() (DamperPosition, bool) {
	h := cache.get("damperpos")
	th, ok := h.(*DamperPosition)
	if !ok {
		return DamperPosition{}, false
	}
	return *th, true
}

func statePoller(monArray []uint16) {
	mon_i := 0
	for {
		// called once for all zones
		c1, ok := getZonesConfig()
		if ok {
			cache.update("tstat", c1)
			pf := "mqtt/infinitive"
			var hum uint8
			for zi := range c1.Zones {
				zp := fmt.Sprintf("%s/zone/%d", pf, c1.Zones[zi].ZoneNumber)
				cache.update(zp+"/currentTemp", c1.Zones[zi].CurrentTemp)
				cache.update(zp+"/humidity", c1.Zones[zi].CurrentHumidity)
				hum = c1.Zones[zi].CurrentHumidity
				cache.update(zp+"/coolSetpoint", c1.Zones[zi].CoolSetpoint)
				cache.update(zp+"/heatSetpoint", c1.Zones[zi].HeatSetpoint)
				cache.update(zp+"/fanMode", c1.Zones[zi].FanMode)
				cache.update(zp+"/hold", *c1.Zones[zi].Hold)
				cache.update(zp+"/overrideDuration", c1.Zones[zi].OvrdDuration)
				cache.update(zp+"/preset", c1.Zones[zi].Preset)
			}

			if hum > 0 {
				cache.update(pf+"/humidity", hum)
			}
			cache.update(pf+"/outdoorTemp", c1.OutdoorTemp)
			cache.update(pf+"/mode", c1.Mode)
			// cache.update(pf+"/action", c1.Action) // replaced by action set from snoop messages
			cache.update(pf+"/rawMode", c1.RawMode)
		}

		// rotate through the registoer monitor probes, if any
		if len(monArray) > 0 {
			getRawData(0x2001, []byte{ 0x00, byte(monArray[mon_i] >> 8 & 0xff), byte(monArray[mon_i] & 0xff) })
			mon_i = (mon_i + 1) % len(monArray)
		}

		time.Sleep(time.Second * 1)
	}
}

func statsPoller() {
	for {
		// called once for all zones
		ss := infinity.getStatsString()
		log.Info("#STATS# ", ss)

		time.Sleep(time.Second * 15)
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
				cache.update("mqtt/infinitive/coilTemp", heatPump.CoilTemp)
				cache.update("mqtt/infinitive/outsideTemp", heatPump.OutsideTemp)
			} else if bytes.Equal(frame.data[0:3], []byte{0x00, 0x3e, 0x02}) {
				heatPump.Stage = data[0] >> 1
				log.Debugf("HP stage is: %d", heatPump.Stage)
				cache.update("heatpump", &heatPump)
				cache.update("mqtt/infinitive/acStage", heatPump.Stage)
			}
		}
	})

	// Snoop Air Handler responses
	infinity.snoopResponse(0x4000, 0x42ff, func(frame *InfinityFrame) {
		data := frame.data[3:]
		airHandler, ok := getAirHandler()
		if ok {
			if bytes.Equal(frame.data[0:3], []byte{0x00, 0x03, 0x06}) {
				airHandler.BlowerRPM = binary.BigEndian.Uint16(data[1:3])
				log.Debugf("blower RPM is: %d", airHandler.BlowerRPM)
				cache.update("blower", &airHandler)
				cache.update("mqtt/infinitive/blowerRPM", airHandler.BlowerRPM)
			} else if bytes.Equal(frame.data[0:3], []byte{0x00, 0x03, 0x16}) {
				airHandler.HeatStage = uint8(data[0])
				airHandler.AirFlowCFM = binary.BigEndian.Uint16(data[4:6])
				airHandler.StaticPressure = float32(float32(int(float32(binary.BigEndian.Uint16(data[7:9])) / float32(65536) * 10000 + 0.5))/10000.0)
				airHandler.ElecHeat = data[0]&0x03 != 0
				switch {
				case data[2] & 0x03 != 0:
					airHandler.Action = "cooling"
				case data[0] & 0x03 != 0:
					airHandler.Action = "heating"
				default:
					airHandler.Action = "idle"
				}
				log.Debugf("air flow CFM is: %d", airHandler.AirFlowCFM)
				cache.update("blower", &airHandler)
				cache.update("mqtt/infinitive/heatStage", airHandler.HeatStage)
				cache.update("mqtt/infinitive/action", airHandler.Action)
				cache.update("mqtt/infinitive/airflowCFM", airHandler.AirFlowCFM)
				cache.update("mqtt/infinitive/staticPressure", airHandler.StaticPressure)
			}
		}
	})

	// Snoop zone controllers 0x6001 and 0x6101 (up to 8 zones total)
	infinity.snoopResponse(0x6000, 0x61ff, func(frame *InfinityFrame) {
		// log.Debug("DamperMsg: ", data)
		data := frame.data[3:]
		damperPos, ok := getDamperPosition()
		if ok {
			if bytes.Equal(frame.data[0:3], []byte{0x00, 0x03, 0x19}) {
				for zi := range damperPos.DamperPos {
					if data[zi] != 0xff {
						damperPos.DamperPos[zi] = uint8(data[zi])
						cache.update(fmt.Sprintf("mqtt/infinitive/zone/%d/damperPos", zi+1), uint(damperPos.DamperPos[zi]) * 100 / 15)
					}
				}
				log.Debug("zone damper positions: ", damperPos.DamperPos)
				cache.update("damperpos", &damperPos)
			}
		}
	})
}


func (l *Logger) Open() (ok bool) {
	ok = true

	tds := time.Now().Format("06010215")
	rlfn := fmt.Sprintf("resplog.%s", tds)
	f, err := os.OpenFile(rlfn, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0755)
	if err != nil {
		log.Errorf("Failed to open resp log file '%s': %s", rlfn, err)
		ok = false
	} else {
		log.Info("Opened resp log file '%s'", rlfn)
		of := l.f
		l.f = f
		l.tds = tds
		if of != nil {
			of.Close()
		}
	}
	l.basems = time.Now().UnixMilli()
	return
}

func (l *Logger) CheckRotate() {
	if l != nil && l.tds != "" && l.tds != time.Now().Format("06010215") {
		l.Open()
	}
}

func (l *Logger) Close() {
	l.CheckRotate()
	if l.f != nil {
		err := l.f.Close()
		if (err != nil) {
			log.Warnf("Error on closing resp logger: %", err)
		} else {
			l.f = nil
		}
	}
}

func (l *Logger) Log(frame *InfinityFrame) {
	l.CheckRotate()
	if l.f != nil {
		l.f.WriteString(fmt.Sprintf("[%s] ", time.Now().Format(time.Stamp)));
		_, err := l.f.WriteString(frame.String())
		if err != nil { log.Error("Logger WriteString failed: ", err) }
		l.f.WriteString("\n")
		err = l.f.Sync()
		if err != nil { log.Error("Logger Sync failed: ", err) }
	}
}

func (l *Logger) LogS(s string) {
	if l.f != nil {
		l.f.WriteString(fmt.Sprintf("[%s] ", time.Now().Format(time.Stamp)));
		_, err := l.f.WriteString(s)
		if err != nil { log.Error("s.Logger WriteString failed: ", err) }
		l.f.WriteString("\n")
		err = l.f.Sync()
		if err != nil { log.Error("s.Logger Sync failed: ", err) }
	}
}

func main() {
	httpPort := flag.Int("httpport", 8080, "HTTP port to listen on")
	serialPort := flag.String("serial", "", "path to serial port")
	mqttBrokerUrl := flag.String("mqtt", "", "url for mqtt broker")
	doRespLog := flag.Bool("rlog", false, "enable resp log")
	doDebugLog := flag.Bool("debug", false, "enable debug log level")

	flag.Parse()

	if len(*serialPort) == 0 {
		fmt.Print("must provide serial\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	loglevel := log.InfoLevel
	if doDebugLog != nil && *doDebugLog { loglevel = log.DebugLevel }
	log.SetLevel(loglevel)

	customFormatter := new(log.TextFormatter)
	customFormatter.TimestampFormat = "2006-01-02 15:04:05"
	customFormatter.FullTimestamp = true
	log.SetFormatter(customFormatter)

	if doRespLog != nil && *doRespLog {
		if !RLogger.Open() {
			panic("unable to open resp log file")
		}
		defer RLogger.Close()
	}

	infinity = &InfinityProtocol{device: *serialPort}
	airHandler := new(AirHandler)
	heatPump := new(HeatPump)
	damperPos := new(DamperPosition)
	cache.update("blower", airHandler)
	cache.update("heatpump", heatPump)
	cache.update("damperpos", damperPos)

	rawMonTable := []uint16{
		// 0x3c01, 0x3c03, 0x3c0a, 0x3c0b, 0x3c0c, 0x3c0d, 0x3c0e, 0x3c0f, 0x3c14, 0x3d02, 0x3d03, 
		0x3b04, 0x3b05, 0x3b06, 0x3b0e, 0x3b0f, 0x3d02, 0x3d03,
	}

	attachSnoops()
	err := infinity.Open()
	if err != nil {
		log.Panicf("error opening serial port: %s", err.Error())
	}

	if mqttBrokerUrl != nil {
		ConnectMqtt(*mqttBrokerUrl, os.Getenv("MQTTPASS"))
	}

	go statePoller(rawMonTable)
	go statsPoller()
	webserver(*httpPort)
}
