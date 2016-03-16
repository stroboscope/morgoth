package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/influxdata/kapacitor/udf"
	"github.com/influxdata/kapacitor/udf/agent"
	"github.com/nathanielc/morgoth"
	"github.com/nathanielc/morgoth/fingerprinters/jsdiv"
	"github.com/nathanielc/morgoth/fingerprinters/kstest"
	"github.com/nathanielc/morgoth/fingerprinters/sigma"
)

func main() {
	a := agent.New()
	h := newHandler(a)
	a.Handler = h

	log.Println("Starting agent")
	a.Start()
	err := a.Wait()
	if err != nil {
		log.Fatal(err)
	}
}

const (
	defaultMinSupport     = 0.05
	defaultErrorTolerance = 0.1
	defaultConsensus      = 0.5
	defaultNormalCount    = 3
)

type fingerprinterInfo struct {
	init    initFingerprinterFunc
	options *udf.OptionInfo
}

// Function that creates a new instance of a fingerprinter
type createFingerprinterFunc func() morgoth.Fingerprinter

// Init createFingerprinterFunc from udf.OptionValues
type initFingerprinterFunc func(opts []*udf.OptionValue) (createFingerprinterFunc, error)

var fingerprinters = map[string]fingerprinterInfo{
	"sigma": {
		options: &udf.OptionInfo{ValueTypes: []udf.ValueType{udf.ValueType_DOUBLE}},
		init: func(args []*udf.OptionValue) (createFingerprinterFunc, error) {
			deviations := args[0].Value.(*udf.OptionValue_DoubleValue).DoubleValue
			if deviations <= 0 {
				return nil, fmt.Errorf("sigma: deviations must be > 0, got %d", deviations)
			}
			return func() morgoth.Fingerprinter {
				return sigma.New(deviations)
			}, nil
		},
	},
	"kstest": {
		options: &udf.OptionInfo{ValueTypes: []udf.ValueType{udf.ValueType_INT}},
		init: func(args []*udf.OptionValue) (createFingerprinterFunc, error) {
			confidence := args[0].Value.(*udf.OptionValue_IntValue).IntValue
			if confidence < 1 || confidence > 5 {
				return nil, fmt.Errorf("kstest: confidence must be in range [1,5], got %d", confidence)
			}
			return func() morgoth.Fingerprinter {
				return kstest.New(uint(confidence))
			}, nil
		},
	},
	"jsdiv": {
		options: &udf.OptionInfo{ValueTypes: []udf.ValueType{
			udf.ValueType_DOUBLE,
			udf.ValueType_DOUBLE,
			udf.ValueType_INT,
			udf.ValueType_DOUBLE,
		}},
		init: func(args []*udf.OptionValue) (createFingerprinterFunc, error) {
			min := args[0].Value.(*udf.OptionValue_DoubleValue).DoubleValue
			max := args[1].Value.(*udf.OptionValue_DoubleValue).DoubleValue
			nBins := args[2].Value.(*udf.OptionValue_IntValue).IntValue
			pValue := args[3].Value.(*udf.OptionValue_DoubleValue).DoubleValue

			if nBins <= 0 {
				return nil, fmt.Errorf("jsdiv: nBins, arg 3, must be > 0, got %d", nBins)
			}
			if pValue <= 0 || pValue > 1 {
				return nil, fmt.Errorf("jsdiv: pValue, arg 4, must be in range (0,1], got %f", pValue)
			}

			return func() morgoth.Fingerprinter {
				return jsdiv.New(min, max, pValue, int(nBins))
			}, nil
		},
	},
}

// A Kapacitor UDF Handler
type Handler struct {
	field          string
	minSupport     float64
	errorTolerance float64
	normalCount    int
	consensus      float64
	agent          *agent.Agent

	currentBatch *morgoth.Window
	detectors    map[string]*morgoth.Detector

	fingerprinters []createFingerprinterFunc
}

func newHandler(a *agent.Agent) *Handler {
	return &Handler{
		agent:          a,
		minSupport:     defaultMinSupport,
		errorTolerance: defaultErrorTolerance,
		normalCount:    defaultNormalCount,
		consensus:      defaultConsensus,
		detectors:      make(map[string]*morgoth.Detector),
	}
}

// Return the InfoResponse. Describing the properties of this Handler
func (h *Handler) Info() (*udf.InfoResponse, error) {
	options := map[string]*udf.OptionInfo{
		"field":          {ValueTypes: []udf.ValueType{udf.ValueType_STRING}},
		"minSupport":     {ValueTypes: []udf.ValueType{udf.ValueType_DOUBLE}},
		"errorTolerance": {ValueTypes: []udf.ValueType{udf.ValueType_DOUBLE}},
		"normalCount":    {ValueTypes: []udf.ValueType{udf.ValueType_INT}},
		"consensus":      {ValueTypes: []udf.ValueType{udf.ValueType_DOUBLE}},
	}
	// Add in options from fingerprinters
	for name, info := range fingerprinters {
		options[name] = info.options
	}
	info := &udf.InfoResponse{
		Wants:    udf.EdgeType_BATCH,
		Provides: udf.EdgeType_STREAM,
		Options:  options,
	}
	return info, nil

}

// Initialize the Handler with the provided options.
func (h *Handler) Init(r *udf.InitRequest) (*udf.InitResponse, error) {
	init := &udf.InitResponse{
		Success: true,
	}
	var errors []string
	for _, opt := range r.Options {
		switch opt.Name {
		case "field":
			h.field = opt.Values[0].Value.(*udf.OptionValue_StringValue).StringValue
		case "minSupport":
			h.minSupport = opt.Values[0].Value.(*udf.OptionValue_DoubleValue).DoubleValue
		case "errorTolerance":
			h.errorTolerance = opt.Values[0].Value.(*udf.OptionValue_DoubleValue).DoubleValue
		case "normalCount":
			h.normalCount = int(opt.Values[0].Value.(*udf.OptionValue_IntValue).IntValue)
		case "consensus":
			h.consensus = opt.Values[0].Value.(*udf.OptionValue_DoubleValue).DoubleValue
		default:
			if info, ok := fingerprinters[opt.Name]; ok {
				createFn, err := info.init(opt.Values)
				if err != nil {
					init.Success = false
					errors = append(errors, err.Error())
				} else {
					h.fingerprinters = append(h.fingerprinters, createFn)
				}
			} else {
				return nil, fmt.Errorf("received unknown init option %q", opt.Name)
			}
		}
	}

	if h.field == "" {
		init.Success = false
		errors = append(errors, "field must not be empty")
	}
	if h.minSupport < 0 || h.minSupport > 1 {
		init.Success = false
		errors = append(errors, "minSupport must be in the range [0,1)")
	}
	if h.errorTolerance < 0 || h.errorTolerance > 1 {
		init.Success = false
		errors = append(errors, "errorTolerance must be in the range [0,1)")
	}
	if h.normalCount <= 1 {
		init.Success = false
		errors = append(errors, "normalCount must be greater than 1")
	}
	if h.consensus < 0 || h.consensus > 1 {
		init.Success = false
		errors = append(errors, "consensus must be in the range [0,1)")
	}
	init.Error = strings.Join(errors, "\n")

	return init, nil
}

// Create a snapshot of the running state of the handler.
func (h *Handler) Snaphost() (*udf.SnapshotResponse, error) {
	return &udf.SnapshotResponse{}, nil
}

// Restore a previous snapshot.
func (h *Handler) Restore(*udf.RestoreRequest) (*udf.RestoreResponse, error) {
	return &udf.RestoreResponse{}, nil
}

// A batch has begun.
func (h *Handler) BeginBatch(b *udf.BeginBatch) error {
	h.currentBatch = &morgoth.Window{
		Name: b.Name,
		Tags: b.Tags,
	}
	return nil
}

// A point has arrived.
func (h *Handler) Point(p *udf.Point) error {
	var value float64
	if f, ok := p.FieldsDouble[h.field]; ok {
		value = f
	} else {
		if i, ok := p.FieldsInt[h.field]; ok {
			value = float64(i)
		} else {
			return fmt.Errorf("no field %s is not a float or int", h.field)
		}
	}
	if h.currentBatch.Start.IsZero() {
		h.currentBatch.Start = time.Unix(0, p.Time).UTC()
	}
	h.currentBatch.Data = append(h.currentBatch.Data, value)
	return nil
}

// The batch is complete.
func (h *Handler) EndBatch(b *udf.EndBatch) error {
	h.currentBatch.Stop = time.Unix(0, b.Tmax).UTC()
	detector, ok := h.detectors[b.Group]
	if !ok {
		detector = morgoth.NewDetector(
			h.normalCount,
			h.consensus,
			h.minSupport,
			h.errorTolerance,
			h.newFingerprinters(),
		)
		h.detectors[b.Group] = detector
	}
	if anomalous, value := detector.IsAnomalous(h.currentBatch); anomalous {
		// Send point back to Kapacitor for anomaly
		dims := make([]string, 0, len(b.Tags))
		for t := range b.Tags {
			dims = append(dims, t)
		}
		p := &udf.Point{
			Name:       b.Name,
			Time:       b.Tmax,
			Tags:       b.Tags,
			Dimensions: dims,
			FieldsDouble: map[string]float64{
				"value": value,
			},
		}
		h.agent.Responses <- &udf.Response{
			Message: &udf.Response_Point{
				Point: p,
			},
		}
	}
	return nil
}

// Gracefully stop the Handler.
// No other methods will be called.
func (h *Handler) Stop() {
	close(h.agent.Responses)
}

func (h *Handler) newFingerprinters() []morgoth.Fingerprinter {
	f := make([]morgoth.Fingerprinter, len(h.fingerprinters))
	for i, create := range h.fingerprinters {
		f[i] = create()
	}
	return f
}
