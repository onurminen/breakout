package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const prometheusURL = "http://localhost:9090"

// series is one time series from a Prometheus query; a single point for instant queries.
type series struct {
	labels map[string]string
	points []point
}

type point struct {
	time  time.Time
	value float64
}

// queryRange evaluates expr from start to end, one point per step.
func queryRange(expr string, start, end time.Time, step time.Duration) []series {
	return prometheusQuery("/api/v1/query_range", url.Values{
		"query": {expr},
		"start": {unixSeconds(start)},
		"end":   {unixSeconds(end)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	})
}

// value evaluates expr at a moment and returns its single value, or NaN if the result is empty.
func value(expr string, at time.Time) float64 {
	result := prometheusQuery("/api/v1/query", url.Values{"query": {expr}, "time": {unixSeconds(at)}})
	if len(result) == 0 {
		return math.NaN()
	}
	return result[0].points[0].value
}

// valueNow is value at the current time; it is 0 instead of NaN for an empty result.
func valueNow(expr string) float64 {
	v := value(expr, time.Now())
	if math.IsNaN(v) {
		return 0
	}
	return v
}

func prometheusQuery(path string, parameters url.Values) []series {
	response, err := http.Get(prometheusURL + path + "?" + parameters.Encode())
	if err != nil {
		fail("query Prometheus: %v", err)
	}
	defer response.Body.Close()
	var body struct {
		Status string
		Error  string
		Data   struct {
			Result []struct {
				Metric map[string]string
				Value  []any   // instant query: [time, "value"]
				Values [][]any // range query: [[time, "value"], ...]
			}
		}
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		fail("decode Prometheus response: %v", err)
	}
	if body.Status != "success" {
		fail("Prometheus query %s: %s", parameters.Get("query"), body.Error)
	}
	var result []series
	for _, r := range body.Data.Result {
		s := series{labels: r.Metric}
		if r.Value != nil {
			r.Values = append(r.Values, r.Value)
		}
		for _, v := range r.Values {
			parsed, _ := strconv.ParseFloat(v[1].(string), 64) // "NaN" parses too
			s.points = append(s.points, point{time.UnixMilli(int64(v[0].(float64) * 1000)), parsed})
		}
		result = append(result, s)
	}
	return result
}

func unixSeconds(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', 3, 64)
}

// timeSeriesQuery is one curve in a run's time series file.
type timeSeriesQuery struct {
	name string
	expr string
}

// writeTimeSeries saves the queries' values over a run as CSV rows of seconds
// since start, series name and value; a query with several series names each
// by its labels.
func writeTimeSeries(path string, queries []timeSeriesQuery, start, end time.Time, step time.Duration) {
	start = start.Truncate(time.Millisecond) // Prometheus takes times in milliseconds
	var rows [][]string
	for _, q := range queries {
		for _, s := range queryRange(q.expr, start, end, step) {
			name := q.name
			if labels := labelString(s.labels); labels != "" {
				name += " " + labels
			}
			for _, p := range s.points {
				if math.IsNaN(p.value) {
					continue
				}
				rows = append(rows, []string{
					strconv.FormatFloat(p.time.Sub(start).Seconds(), 'f', 2, 64),
					name,
					strconv.FormatFloat(p.value, 'g', 6, 64),
				})
			}
		}
	}
	writeCSV(path, []string{"seconds", "series", "value"}, rows, false)
}

func labelString(labels map[string]string) string {
	var parts []string
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// writeCSV writes rows to path. With appendRows, rows are added to an existing
// file and the header is written only if the file is new.
func writeCSV(path string, header []string, rows [][]string, appendRows bool) {
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendRows {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		fail("%v", err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if info, _ := file.Stat(); info.Size() == 0 {
		writer.Write(header)
	}
	writer.WriteAll(rows)
	if err := writer.Error(); err != nil {
		fail("write %s: %v", path, err)
	}
}

func format(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }

func fail(message string, args ...any) {
	fmt.Fprintf(os.Stderr, "experiments: "+message+"\n", args...)
	os.Exit(1)
}
