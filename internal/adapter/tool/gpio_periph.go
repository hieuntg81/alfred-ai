//go:build edge

package tool

import (
	"fmt"
	"sort"
	"sync"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/host/v3"
)

// PeriphGPIOBackend implements GPIOBackend using periph.io for real hardware GPIO.
type PeriphGPIOBackend struct {
	mu   sync.Mutex
	pins map[int]gpio.PinIO // cached pin handles
}

// NewPeriphGPIOBackend initializes periph.io and returns a real GPIO backend.
// Returns an error if periph.io host initialization fails.
func NewPeriphGPIOBackend() (*PeriphGPIOBackend, error) {
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("periph host init: %w", err)
	}
	return &PeriphGPIOBackend{
		pins: make(map[int]gpio.PinIO),
	}, nil
}

// resolvePin looks up a GPIO pin by number, caching the result.
func (b *PeriphGPIOBackend) resolvePin(pin int) (gpio.PinIO, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if p, ok := b.pins[pin]; ok {
		return p, nil
	}

	name := fmt.Sprintf("GPIO%d", pin)
	p := gpioreg.ByName(name)
	if p == nil {
		return nil, fmt.Errorf("pin %d (%s) not found in hardware", pin, name)
	}
	b.pins[pin] = p
	return p, nil
}

func (b *PeriphGPIOBackend) Read(pin int) (int, error) {
	p, err := b.resolvePin(pin)
	if err != nil {
		return 0, err
	}
	if err := p.In(gpio.PullNoChange, gpio.NoEdge); err != nil {
		return 0, fmt.Errorf("set pin %d to input: %w", pin, err)
	}
	if p.Read() == gpio.High {
		return 1, nil
	}
	return 0, nil
}

func (b *PeriphGPIOBackend) Write(pin, value int) error {
	p, err := b.resolvePin(pin)
	if err != nil {
		return err
	}
	level := gpio.Low
	if value != 0 {
		level = gpio.High
	}
	return p.Out(level)
}

func (b *PeriphGPIOBackend) SetPWM(pin int, dutyCycle float64) error {
	p, err := b.resolvePin(pin)
	if err != nil {
		return err
	}
	// Convert duty cycle (0.0-1.0) to gpio.Duty (0-DutyMax).
	duty := gpio.Duty(dutyCycle * float64(gpio.DutyMax))
	// Default PWM frequency of 1kHz.
	return p.PWM(duty, 1000*physic.Hertz)
}

func (b *PeriphGPIOBackend) ListPins() []GPIOPinInfo {
	all := gpioreg.All()
	var pins []GPIOPinInfo
	for _, p := range all {
		var num int
		if _, err := fmt.Sscanf(p.Name(), "GPIO%d", &num); err != nil {
			continue // skip non-GPIO pins (e.g. named aliases)
		}
		info := GPIOPinInfo{
			Pin:  num,
			Mode: periphPinMode(p),
		}
		if p.Read() == gpio.High {
			info.Value = 1
		}
		pins = append(pins, info)
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Pin < pins[j].Pin })
	return pins
}

// periphPinMode returns the current mode of a pin as a human-readable string.
func periphPinMode(p gpio.PinIO) string {
	fn := p.Function()
	switch fn {
	case "In", "":
		return "input"
	case "Out":
		return "output"
	default:
		return fn
	}
}
