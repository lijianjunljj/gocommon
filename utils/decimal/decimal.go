package decimal

import (
	"github.com/shopspring/decimal"
)

func Add(round int32, args ...float64) float64 {
	d := decimal.NewFromFloat(0)
	for _, v := range args {
		d = d.Add(decimal.NewFromFloat(v))
	}
	v, _ := d.Round(round).Float64()
	return v
}

func Mul(round int32, args ...float64) float64 {
	d := decimal.NewFromFloat(1)
	for _, v := range args {
		d = d.Mul(decimal.NewFromFloat(v))
	}
	v, _ := d.Round(round).Float64()
	return v

}

func Sub(round int32, args ...float64) float64 {
	d := decimal.NewFromFloat(args[0])
	for _, v := range args {
		d = d.Sub(decimal.NewFromFloat(v))
	}
	v, _ := d.Round(round).Float64()
	return v
}

func Div(round int32, args ...float64) float64 {
	d := decimal.NewFromFloat(args[0])
	for _, v := range args {
		d = d.Div(decimal.NewFromFloat(v))
	}
	v, _ := d.Round(round).Float64()
	return v

}
