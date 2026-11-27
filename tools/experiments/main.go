// Command experiments runs the experiments of docs/evaluation.md on the Docker
// Compose stack and writes their results as CSV files to report/data/.
//
//	go run ./tools/experiments e1   # engine scale-out: 1, 2, 4, 8 engines
//	go run ./tools/experiments e2   # one engine with 0.5, 1, 2, 4 CPUs
//	go run ./tools/experiments e3   # replay speed and latency
//	go run ./tools/experiments e3-fast # one maximum-speed replay and latency run
//	go run ./tools/experiments e2e  # end-to-end correctness check
//
// Every experiment starts from an empty stack (docker compose down). E1, E2 and
// the end-to-end check replay the whole week, E3 Monday.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	runsPerSetting = 3
	outputDir      = "report/data"
	databaseURL    = "postgres://breakout:breakout@localhost:5432/breakout"

	// Paced E3 runs replay Monday's night as fast as possible, then measure this
	// long from 09:00 on, during trading hours.
	pacedFrom     = "2021-11-08T09:00:00Z"
	pacedDuration = 10 * time.Minute

	// Two equities and two indices from each exchange, all among Monday's most
	// active. Their first real crossovers come on Wednesday, after the warm-up.
	endToEndSymbols = "823212.ETR,766403.ETR,A0X7J3.ETR,A2FG24.ETR,MC.FR,TTE.FR,TCAMB.FR,ECC3D.FR,ASML.NL,RDSA.NL,ENVG.NL,ENVN.NL"
)

const topicRecordsQuery = `sum(kafka_topic_partition_current_offset{topic="ticks"})`

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) != 2 {
		fail("usage: go run ./tools/experiments e1|e2|e3|e3-fast|e2e")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fail("%v", err)
	}
	switch os.Args[1] {
	case "e1":
		prefilled("e1", []engineSetting{{1, ""}, {2, ""}, {4, ""}, {8, ""}})
	case "e2":
		prefilled("e2", []engineSetting{{1, "0.5"}, {1, "1"}, {1, "2"}, {1, "4"}})
	case "e3":
		replaySpeeds()
	case "e3-fast":
		replaySpeedFast()
	case "e2e":
		endToEnd()
	default:
		fail("unknown experiment %q; want e1, e2, e3, e3-fast or e2e", os.Args[1])
	}
}

type engineSetting struct {
	engines int
	cpus    string // CPU limit per engine container; "" = none
}

// prefilled measures how fast the engines alone process the whole week.
// Ingestion first fills ticks; then each run starts the engines on the full
// topic. Engines commit no offsets, so every run reads the topic from the start.
func prefilled(name string, settings []engineSetting) {
	days, containerPaths := weekFiles()
	cleanStart()
	log.Printf("filling ticks with %d days", len(days))
	compose(map[string]string{"INGEST_FILES": containerPaths}, "up", "-d", "ingestion")
	waitForIngestion(30 * time.Minute)
	time.Sleep(10 * time.Second) // kafka-exporter reports the final offsets
	topicRecords := valueNow(topicRecordsQuery)

	summaryPath := filepath.Join(outputDir, name+"-summary.csv")
	os.Remove(summaryPath)
	header := []string{"engines", "engine_cpus", "run", "records", "events", "seconds", "records_per_s", "events_per_s",
		"engine_cpu_seconds", "engine_cores", "peak_engine_memory_bytes"}
	for _, setting := range settings {
		for run := 1; run <= runsPerSetting; run++ {
			writeCSV(summaryPath, header, [][]string{prefilledRun(name, setting, run, topicRecords)}, true)
		}
	}
	log.Printf("wrote %s", summaryPath)
}

func prefilledRun(name string, setting engineSetting, run int, topicRecords float64) []string {
	log.Printf("%s: %d engines, CPU limit %q, run %d", name, setting.engines, setting.cpus, run)
	compose(nil, "rm", "--stop", "--force", "engine")
	// A finished engine's counter already equals the topic size; wait until
	// Prometheus has dropped the old replicas' series.
	waitFor("old engine series to go stale", time.Minute, func() bool { return valueNow("count(engine_records_total)") == 0 })

	start := time.Now()
	compose(map[string]string{"ENGINE_CPUS": setting.cpus}, "up", "-d", "--scale", fmt.Sprint("engine=", setting.engines), "engine")
	// cAdvisor keeps reporting removed containers under the same names, so this
	// run's containers are selected by their IDs.
	engineContainers := `id=~".*(` + strings.Join(strings.Fields(composeOutput("ps", "-q", "engine")), "|") + `).*"`
	waitFor("engines to read the topic", 30*time.Minute, func() bool { return valueNow("sum(engine_records_total)") >= topicRecords })
	time.Sleep(15 * time.Second) // the last scrapes of the engines and cAdvisor
	end := time.Now()

	records := queryRange("sum(engine_records_total)", start, end, scrapeStep)[0].points
	events := queryRange("sum(engine_events_total)", start, end, scrapeStep)[0].points
	if final := records[len(records)-1].value; final != topicRecords {
		fail("engines read %.0f records, but ticks holds %.0f: partitions were re-read after a rebalance", final, topicRecords)
	}
	first, done := activeSpan(records, topicRecords)
	seconds := records[done].time.Sub(records[first].time).Seconds()
	recordsPerSecond := (records[done].value - records[first].value) / seconds
	eventsPerSecond := (eventsAt(events, records[done].time) - eventsAt(events, records[first].time)) / seconds

	cpuSeconds := value(`sum(container_cpu_usage_seconds_total{`+engineContainers+`})`, end)
	peakMemory := 0.0
	for _, p := range queryRange(`sum(container_memory_working_set_bytes{`+engineContainers+`})`, start, end, scrapeStep)[0].points {
		peakMemory = max(peakMemory, p.value)
	}

	label := fmt.Sprintf("engines%d", setting.engines)
	if setting.cpus != "" {
		label = "cpus" + setting.cpus
	}
	writeTimeSeries(filepath.Join(outputDir, fmt.Sprintf("%s-%s-run%d.csv", name, label, run)), []timeSeriesQuery{
		{"events_per_s", "sum(rate(engine_events_total[3s]))"},
		{"cpu_cores", `sum by (name) (rate(container_cpu_usage_seconds_total{` + engineContainers + `}[10s]))`},
		{"memory_bytes", `sum by (name) (container_memory_working_set_bytes{` + engineContainers + `})`},
	}, start, end, scrapeStep)

	log.Printf("%.0f events/s, %.2f s", eventsPerSecond, seconds)
	return []string{
		fmt.Sprint(setting.engines), setting.cpus, fmt.Sprint(run),
		format(topicRecords), format(events[len(events)-1].value), format(seconds),
		format(recordsPerSecond), format(eventsPerSecond),
		format(cpuSeconds), format(cpuSeconds / seconds), format(peakMemory),
	}
}

// scrapeStep matches the engine's scrape interval in deploy/prometheus/prometheus.yml.
const scrapeStep = 250 * time.Millisecond

// activeSpan returns the indexes of the first point above zero and the first
// point that reaches total. Measuring between two scrapes, not from the start
// command, leaves out container startup and joining the consumer group.
func activeSpan(points []point, total float64) (first, done int) {
	first, done = -1, -1
	for i, p := range points {
		if first < 0 && p.value > 0 {
			first = i
		}
		if done < 0 && p.value >= total {
			done = i
		}
	}
	if first < 0 || done <= first {
		fail("run too short to measure at the scrape interval")
	}
	return first, done
}

func eventsAt(points []point, at time.Time) float64 {
	for _, p := range points {
		if p.time.Equal(at) {
			return p.value
		}
	}
	return math.NaN()
}

// replaySpeeds runs the whole pipeline (one engine and the sink) live at
// several replay speeds and measures both latencies. "0" is as fast as possible.
func replaySpeeds() {
	summaryPath := filepath.Join(outputDir, "e3-summary.csv")
	os.Remove(summaryPath)
	header := []string{"speed", "run", "seconds", "events", "events_per_s",
		"event_latency_p50", "event_latency_p95", "event_latency_p99",
		"window_latency_p50", "window_latency_p95", "window_latency_p99",
		"ingestion_cores", "engine_cores", "sink_cores", "kafka_cores"}
	for _, speed := range []string{"1", "10", "60", "0"} {
		for run := 1; run <= runsPerSetting; run++ {
			writeCSV(summaryPath, header, [][]string{liveRun("e3", speed, run)}, true)
		}
	}
	log.Printf("wrote %s", summaryPath)
}

// replaySpeedFast runs the maximum-speed E3 setting once. It is intended for
// a quick evaluation check; the full E3 experiment remains the three-run,
// four-speed benchmark.
func replaySpeedFast() {
	summaryPath := filepath.Join(outputDir, "e3-fast-summary.csv")
	os.Remove(summaryPath)
	header := []string{"speed", "run", "seconds", "events", "events_per_s",
		"event_latency_p50", "event_latency_p95", "event_latency_p99",
		"window_latency_p50", "window_latency_p95", "window_latency_p99",
		"ingestion_cores", "engine_cores", "sink_cores", "kafka_cores"}
	writeCSV(summaryPath, header, [][]string{liveRun("e3-fast", "0", 1)}, true)
	log.Printf("wrote %s", summaryPath)
}

func liveRun(experiment, speed string, run int) []string {
	log.Printf("%s: speed %s, run %d", experiment, speed, run)
	cleanStart()
	compose(nil, "up", "-d", "engine", "sink")
	time.Sleep(10 * time.Second) // joining the group, including Kafka's initial rebalance delay

	env := map[string]string{"REPLAY_SPEED": speed}
	if speed != "0" {
		env["REPLAY_FROM"] = pacedFrom
	}
	launched := time.Now()
	compose(env, "up", "-d", "ingestion")

	var start, end time.Time
	if speed == "0" {
		waitForIngestion(15 * time.Minute)
		time.Sleep(10 * time.Second)
		topicRecords := valueNow(topicRecordsQuery)
		waitFor("engine to read the topic", 15*time.Minute, func() bool { return valueNow("sum(engine_records_total)") >= topicRecords })
		time.Sleep(5 * time.Second)
		records := queryRange("sum(engine_records_total)", launched, time.Now(), scrapeStep)[0].points
		first, done := activeSpan(records, topicRecords)
		start, end = records[first].time, records[done].time
	} else {
		pacedFromMs := mustParseTime(pacedFrom).UnixMilli()
		waitFor("pacing to start", 10*time.Minute, func() bool { return valueNow("max(ingest_event_time_ms)") >= float64(pacedFromMs) })
		start = time.Now()
		time.Sleep(pacedDuration)
		end = time.Now()
	}
	time.Sleep(10 * time.Second) // the last scrapes

	seconds := end.Sub(start).Seconds()
	window := fmt.Sprintf("[%ds]", int(math.Round(seconds)))
	at := func(expr string) float64 { return value(expr, end) }
	quantile := func(q float64, histogram string) string {
		return format(at(fmt.Sprintf("histogram_quantile(%g, sum by (le) (increase(%s_bucket%s)))", q, histogram, window)))
	}
	cores := func(container string) string {
		return format(at(`sum(rate(container_cpu_usage_seconds_total{name=~"breakout-` + container + `-.+"}` + window + `))`))
	}
	events := at("sum(increase(engine_events_total" + window + "))")

	writeTimeSeries(filepath.Join(outputDir, fmt.Sprintf("%s-speed%s-run%d.csv", experiment, speed, run)), []timeSeriesQuery{
		{"events_per_s", "sum(rate(engine_events_total[15s]))"},
		{"event_latency_p50", "histogram_quantile(0.5, sum by (le) (rate(engine_event_latency_seconds_bucket[30s])))"},
		{"event_latency_p95", "histogram_quantile(0.95, sum by (le) (rate(engine_event_latency_seconds_bucket[30s])))"},
		{"event_latency_p99", "histogram_quantile(0.99, sum by (le) (rate(engine_event_latency_seconds_bucket[30s])))"},
		{"window_latency_p50", "histogram_quantile(0.5, sum by (le) (rate(engine_window_result_latency_seconds_bucket[30s])))"},
		{"window_latency_p95", "histogram_quantile(0.95, sum by (le) (rate(engine_window_result_latency_seconds_bucket[30s])))"},
		{"window_latency_p99", "histogram_quantile(0.99, sum by (le) (rate(engine_window_result_latency_seconds_bucket[30s])))"},
		{"cpu_cores", `sum by (name) (rate(container_cpu_usage_seconds_total{name=~"breakout-.+"}[30s]))`},
	}, start, end, 5*time.Second)

	log.Printf("%.0f events/s over %.0f s", events/seconds, seconds)
	return []string{
		speed, fmt.Sprint(run), format(seconds), format(events), format(events / seconds),
		quantile(0.5, "engine_event_latency_seconds"), quantile(0.95, "engine_event_latency_seconds"), quantile(0.99, "engine_event_latency_seconds"),
		quantile(0.5, "engine_window_result_latency_seconds"), quantile(0.95, "engine_window_result_latency_seconds"), quantile(0.99, "engine_window_result_latency_seconds"),
		cores("ingestion"), cores("engine"), cores("sink"), cores("kafka"),
	}
}

// endToEnd replays the whole week for a dozen symbols through the pipeline and
// compares what reached Postgres with tools/enginecheck's results, row by row.
func endToEnd() {
	days, containerPaths := weekFiles()
	cleanStart()
	compose(nil, "up", "-d", "engine", "sink")
	time.Sleep(10 * time.Second)
	compose(map[string]string{"SYMBOLS": endToEndSymbols, "INGEST_FILES": containerPaths}, "up", "-d", "ingestion")
	waitForIngestion(30 * time.Minute)
	time.Sleep(10 * time.Second)
	topicRecords := valueNow(topicRecordsQuery)
	waitFor("engine to read the topic", 5*time.Minute, func() bool { return valueNow("sum(engine_records_total)") >= topicRecords })
	waitFor("sink to store every result", 5*time.Minute, func() bool {
		results := valueNow("sum(engine_results_total)")
		return results > 0 && valueNow(`sum(sink_rows_total{table="ema_results"})`) == results
	})

	log.Print("comparing Postgres with tools/enginecheck")
	check := exec.Command("go", append([]string{"run", "./tools/enginecheck", "-symbols", endToEndSymbols, "-db", databaseURL}, days...)...)
	check.Stdout, check.Stderr = os.Stdout, os.Stderr
	if err := check.Run(); err != nil {
		fail("end-to-end check failed: %v", err)
	}
}

// weekFiles returns the 7 day files in date order, as paths on the host and as
// INGEST_FILES, the same files inside the ingestion container.
func weekFiles() (hostPaths []string, containerPaths string) {
	hostPaths, _ = filepath.Glob("data/debs2022-gc-trading-day-*.csv") // sorted, so in date order
	if len(hostPaths) != 7 {
		fail("want the 7 day files in data/, found %d", len(hostPaths))
	}
	var paths []string
	for _, path := range hostPaths {
		paths = append(paths, "/data/"+filepath.Base(path))
	}
	return hostPaths, strings.Join(paths, " ")
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		fail("%v", err)
	}
	return t
}
