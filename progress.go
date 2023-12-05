package bifrost

import (
	"fmt"
	"time"
)

type Progress struct {
	Start     time.Time
	LastPrint time.Time
	Current   uint64
	Total     uint64
}

func (p *Progress) Reset(total uint64) {
	p.Start = time.Now()
	p.Current = 0
	p.Total = total
	p.LastPrint = time.Now()
}

func (p *Progress) Increment() {
	p.Current++
}

// Print prints the current progress bar to the console with current, total, percentage and ETA
func (p *Progress) Print() {
	if time.Since(p.LastPrint) < time.Second {
		return
	}
	p.LastPrint = time.Now()
	fmt.Printf("\r%v/%v (%.2f%%) ETA: %v", p.Current, p.Total, float64(p.Current)/float64(p.Total)*100, p.ETA())
}

// ETA returns the estimated time of arrival
func (p *Progress) ETA() string {
	elapsed := time.Since(p.Start)
	eta := time.Duration(float64(elapsed) / float64(p.Current) * float64(p.Total-p.Current))
	return eta.String()
}
