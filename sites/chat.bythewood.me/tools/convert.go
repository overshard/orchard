package tools

import (
	"context"
	"fmt"
	"strings"
)

// units are grouped so a length never converts into a weight. Volume to weight
// is the one crossing allowed and it's only true for water, so the answer says
// so instead of quietly being wrong about flour.
var (
	weight = map[string]float64{"mg": 0.001, "g": 1, "kg": 1000, "oz": 28.3495, "lb": 453.592, "st": 6350.29}
	volume = map[string]float64{"ml": 1, "l": 1000, "tsp": 4.92892, "tbsp": 14.7868,
		"cup": 236.588, "floz": 29.5735, "pint": 473.176, "quart": 946.353, "gallon": 3785.41}
	length = map[string]float64{"mm": 0.001, "cm": 0.01, "m": 1, "km": 1000,
		"in": 0.0254, "ft": 0.3048, "yd": 0.9144, "mi": 1609.34}
)

var unitAlias = map[string]string{
	"gram": "g", "grams": "g", "gramme": "g", "kilogram": "kg", "kilograms": "kg", "kilo": "kg",
	"ounce": "oz", "ounces": "oz", "pound": "lb", "pounds": "lb", "lbs": "lb",
	"millilitre": "ml", "milliliter": "ml", "litre": "l", "liter": "l", "liters": "l", "litres": "l",
	"teaspoon": "tsp", "teaspoons": "tsp", "tablespoon": "tbsp", "tablespoons": "tbsp",
	"cups": "cup", "fluid ounce": "floz", "fl oz": "floz", "fahrenheit": "f", "celsius": "c",
	"centigrade": "c", "inch": "in", "inches": "in", "foot": "ft", "feet": "ft",
	"mile": "mi", "miles": "mi", "metre": "m", "meter": "m", "kilometre": "km", "kilometer": "km",
}

func normUnit(u string) string {
	u = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(u, ".")))
	u = strings.TrimPrefix(u, "°")
	if v, ok := unitAlias[u]; ok {
		return v
	}
	return u
}

type convertArgs struct {
	Value    float64
	FromUnit string
	ToUnit   string
}

func (a *convertArgs) params() []param {
	return []param{
		numArg(&a.Value, "value", "the number to convert").required(),
		strArg(&a.FromUnit, "from_unit", "the unit it is in, like cup, lb, f, mi").required(),
		strArg(&a.ToUnit, "to_unit", "the unit to convert to, like g, kg, c, km").required(),
	}
}

var Convert = Define(Tool{
	Name:        "convert",
	Description: "Convert between units of weight, volume, length or temperature.",
}, func(ctx context.Context, d *Turn, a convertArgs) (any, error) {
	v := a.Value
	from, to := normUnit(a.FromUnit), normUnit(a.ToUnit)
	if from == "" || to == "" {
		return nil, fmt.Errorf("from_unit and to_unit are required")
	}
	if from == "c" || from == "f" || to == "c" || to == "f" {
		switch {
		case from == "c" && to == "f":
			return map[string]any{"value": round1(v*9/5 + 32), "unit": "F"}, nil
		case from == "f" && to == "c":
			return map[string]any{"value": round1((v - 32) * 5 / 9), "unit": "C"}, nil
		case from == to:
			return map[string]any{"value": v, "unit": strings.ToUpper(from)}, nil
		}
		return nil, fmt.Errorf("temperature only converts to temperature")
	}
	for _, set := range []map[string]float64{weight, volume, length} {
		f, okF := set[from]
		t, okT := set[to]
		if okF && okT {
			return map[string]any{"value": round4(v * f / t), "unit": to}, nil
		}
	}
	// The one crossing worth allowing, said out loud.
	if f, ok := volume[from]; ok {
		if t, ok2 := weight[to]; ok2 {
			return map[string]any{"value": round4(v * f / t), "unit": to,
				"warning": "volume to weight is only right for water. Flour, sugar and butter all differ, so say so or look up the real density."}, nil
		}
	}
	return nil, fmt.Errorf("cannot convert %s to %s", from, to)
})

func round4(f float64) float64 { return float64(int(f*10000+0.5)) / 10000 }
