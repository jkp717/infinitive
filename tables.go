package main

type InfinityTableAddr [3]byte
type InfinityTable interface {
	addr() InfinityTableAddr
}

type TStatCurrentParams struct {
	ZCurrentTemp      [8]uint8
	ZCurrentHumidity  [8]uint8
	Unknown1          uint8
	OutdoorAirTemp    uint8
	ZoneUnocc         uint8 // bitflags
	Mode              uint8
	Unknown2          [5]uint8
	DisplayedZone     uint8
}

func (params TStatCurrentParams) addr() InfinityTableAddr {
	return InfinityTableAddr{0x00, 0x3B, 0x02}
}

type TStatZoneParams struct {
	ZFanMode         [8]uint8
	ZoneHold         uint8 // bitflags
	ZHeatSetpoint    [8]uint8
	ZCoolSetpoint    [8]uint8
	ZTargetHumidity  [8]uint8
	FanAutoCfg       uint8
	Unknown          uint8
	ZHoldDuration    [8]uint16
	ZName            [8][12]byte
}

func (params TStatZoneParams) addr() InfinityTableAddr {
	return InfinityTableAddr{0x00, 0x3B, 0x03}
}

type TStatVacationParams struct {
	Active         uint8
	Hours          uint16
	MinTemperature uint8
	MaxTemperature uint8
	MinHumidity    uint8
	MaxHumidity    uint8
	FanMode        uint8 // matches fan mode from TStatZoneParams
}

func (params TStatVacationParams) addr() InfinityTableAddr {
	return InfinityTableAddr{0x00, 0x3B, 0x04}
}

type APIVacationConfig struct {
	Active         *bool   `json:"active"`
	Days           *uint8  `json:"days"`
	MinTemperature *uint8  `json:"minTemperature"`
	MaxTemperature *uint8  `json:"maxTemperature"`
	MinHumidity    *uint8  `json:"minHumidity"`
	MaxHumidity    *uint8  `json:"maxHumidity"`
	FanMode        *string `json:"fanMode"`
}

func (params TStatVacationParams) toAPI() APIVacationConfig {
	api := APIVacationConfig{MinTemperature: &params.MinTemperature,
		MaxTemperature: &params.MaxTemperature,
		MinHumidity:    &params.MinHumidity,
		MaxHumidity:    &params.MaxHumidity}

	active := bool(params.Active == 1)
	api.Active = &active

	days := uint8(params.Hours / 7)
	api.Days = &days

	mode := rawFanModeToString(params.FanMode)
	api.FanMode = &mode

	return api
}

func (params *TStatVacationParams) fromAPI(config *APIVacationConfig) byte {
	flags := byte(0)

	if config.Days != nil {
		params.Hours = uint16(*config.Days) * uint16(24)
		flags |= 0x02
	}

	if config.MinTemperature != nil {
		params.MinTemperature = *config.MinTemperature
		flags |= 0x04
	}

	if config.MaxTemperature != nil {
		params.MaxTemperature = *config.MaxTemperature
		flags |= 0x08
	}

	if config.MinHumidity != nil {
		params.MinHumidity = *config.MinHumidity
		flags |= 0x10
	}

	if config.MaxHumidity != nil {
		params.MaxHumidity = *config.MaxHumidity
		flags |= 0x20
	}

	if config.FanMode != nil {
		mode, _ := stringFanModeToRaw(*config.FanMode)
		// FIXME: check for ok here
		params.FanMode = mode
		flags |= 0x40
	}

	return flags
}

type TStatSettings struct {
	BacklightSetting uint8
	AutoMode         uint8
	Unknown1         uint8
	DeadBand         uint8
	CyclesPerHour    uint8
	SchedulePeriods  uint8
	ProgramsEnabled  uint8
	TempUnits        uint8
	Unknown2         uint8
	DealerName       [20]byte
	DealerPhone      [20]byte
}

func (params TStatSettings) addr() InfinityTableAddr {
	return InfinityTableAddr{0x00, 0x3B, 0x06}
}
