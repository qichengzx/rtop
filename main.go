package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	"github.com/go-redis/redis/v8"
)

var (
	redisClient    = &redis.Client{}
	ctx            = context.Background()
	addr, password = "", ""
	ch             = make(chan Info, 2)
	ticker         = time.NewTicker(REFRESH * time.Second)
)

const (
	REFRESH = 3
	maxLen  = 50
	CPUSYS  = "sys"
	CPUSER  = "user"
)

type Top struct {
	t    time.Time
	prev *Info
	now  *Info
}

type Info struct {
	t time.Time
	//Server
	Version         string `json:"redis_version"`
	UptimeInSeconds int64  `json:"uptime_in_seconds"`

	//CPU
	CpuSys  float64 `json:"used_cpu_sys"`
	CpuUser float64 `json:"used_cpu_user"`

	//Clients
	ConnectedClients    int64 `json:"connected_clients"`
	ConnectedClientLine []float64
	BlockedClients      int64 `json:"blocked_clients"`
	BlockedClientsLine  []float64

	//Memory
	UsedMemory float64 `json:"used_memory"`
	MaxMemery  float64 `json:"maxmemory"`

	//Persistence
	RdbChangesSinceLastSave int64 `json:"rdb_changes_since_last_save"`
	RdbLastSaveTime         int64 `json:"rdb_last_save_time"`

	//Stats
	Qps                     int64 `json:"instantaneous_ops_per_sec"`
	QpsLine                 []float64
	InstantaneousInputKbps  float64 `json:"instantaneous_input_kbps"`
	InputKbsLine            []float64
	InstantaneousOutputKbps float64 `json:"instantaneous_output_kbps"`
	OutputKbsLine           []float64
	KeyspaceMisses          int64 `json:"keyspace_misses"`
	KeyspaceHits            int64 `json:"keyspace_hits"`

	//Replication
	Role string `json:"role"`
}

func main() {
	flag.StringVar(&addr, "addr", "127.0.0.1:6379", "redis addr, ip:port")
	flag.StringVar(&password, "password", "", "redis auth password")
	flag.Parse()

	redisClient = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		MaxRetries:   3,
		MinIdleConns: 1,
		DialTimeout:  time.Second * 2,
		IdleTimeout:  time.Second * 5,
	})
	var t = &Top{
		t:   time.Now(),
		now: &Info{},
	}
	t.run()
}

func (t *Top) run() {
	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()
	fetch()
	uiEvents := ui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			}
		case <-ticker.C:
			fetch()
		case item, ok := <-ch:
			if ok {
				t.prev = t.now
				t.now = &item
				t.format()
				t.render()
			}
		}
	}
}

func (t *Top) format() {
	if len(t.prev.QpsLine) > maxLen-1 {
		d := len(t.prev.QpsLine) - maxLen
		t.prev.QpsLine = t.prev.QpsLine[d:]
	}
	t.now.QpsLine = append(t.prev.QpsLine, float64(t.now.Qps))

	if len(t.prev.InputKbsLine) > maxLen {
		d := len(t.prev.InputKbsLine) - maxLen
		t.prev.InputKbsLine = t.prev.InputKbsLine[d:]
	}
	t.now.InputKbsLine = append(t.prev.InputKbsLine, t.now.InstantaneousInputKbps)

	if len(t.prev.OutputKbsLine) > maxLen {
		d := len(t.prev.OutputKbsLine) - maxLen
		t.prev.OutputKbsLine = t.prev.OutputKbsLine[d:]
	}
	t.now.OutputKbsLine = append(t.prev.OutputKbsLine, t.now.InstantaneousOutputKbps)

	if len(t.prev.ConnectedClientLine) > maxLen {
		d := len(t.prev.ConnectedClientLine) - maxLen
		t.prev.ConnectedClientLine = t.prev.ConnectedClientLine[d:]
	}
	t.now.ConnectedClientLine = append(t.prev.ConnectedClientLine, float64(t.now.ConnectedClients))

	if len(t.prev.BlockedClientsLine) > maxLen {
		d := len(t.prev.BlockedClientsLine) - maxLen
		t.prev.BlockedClientsLine = t.prev.BlockedClientsLine[d:]
	}
	t.now.BlockedClientsLine = append(t.prev.BlockedClientsLine, float64(t.now.BlockedClients))
}

func fetch() {
	r, err := redisClient.Info(ctx).Result()
	if err != nil {
		panic(err)
	}

	infoSlice := strings.Split(r, "\r\n")
	var m = make(map[string]interface{}, len(infoSlice))
	for _, str := range infoSlice {
		if strings.HasPrefix(str, "#") || str == "" {
			continue
		}

		kv := parseStr(str)
		m[kv.k] = kv.formatVal()
	}
	j, _ := json.Marshal(m)
	var info = Info{
		t: time.Now(),
	}
	err = json.Unmarshal(j, &info)
	if err != nil {
		return
	}
	ch <- info
}

// calCpuSys cal cpu use in sys
func (t Top) calCpuSys() float64 {
	f := t.diffCpu(CPUSYS)
	return t.calCpu(f)
}

// calCpuUs cal cpu use in user
func (t Top) calCpuUs() float64 {
	f := t.diffCpu(CPUSER)
	return t.calCpu(f)
}

// ((used_cpu_sys_now-used_cpu_sys_before)/(now-before))*100
func (t Top) calCpu(f float64) float64 {
	if t.prev == nil {
		return 0
	}

	tm := t.now.t.Sub(t.prev.t).Seconds()
	n := (f / tm) * 100
	if math.IsNaN(n) {
		n = 0
	}
	return n
}

func (t Top) diffCpu(ty string) float64 {
	if t.prev == nil {
		return 0
	}
	if ty == CPUSER {
		return t.now.CpuUser - t.prev.CpuUser
	}

	return t.now.CpuSys - t.prev.CpuSys
}

// calMemory cal memory used in percent
func (t Top) calMemory() float64 {
	if t.now.MaxMemery == 0 {
		return 0
	}
	n := (t.now.UsedMemory / t.now.MaxMemery) * 100
	if math.IsNaN(n) {
		n = 0
	}
	return n
}

type property struct {
	k string
	v string
}

// parseStr parse redis info string
func parseStr(x string) property {
	slice := strings.SplitN(x, ":", 2)
	if len(slice) < 2 {
		return property{}
	}

	return property{
		k: slice[0],
		v: slice[1],
	}
}

// formatVal format info val
func (p property) formatVal() interface{} {
	var v interface{}
	switch p.k {
	case "used_cpu_sys", "used_cpu_user", "instantaneous_input_kbps", "instantaneous_output_kbps",
		"maxmemory", "used_memory":
		v = p.float64()

	case "uptime_in_seconds", "connected_clients", "rdb_changes_since_last_save", "rdb_last_save_time",
		"instantaneous_ops_per_sec", "keyspace_hits", "keyspace_misses", "blocked_clients":
		v = p.int64()

	default:
		v = p.v
	}

	return v
}

func (p property) int64() int64 {
	i, _ := strconv.ParseInt(p.v, 10, 64)
	return i
}

func (p property) float64() float64 {
	f, _ := strconv.ParseFloat(p.v, 64)
	return f
}

func (t *Top) render() {
	summary := renderSummary(t)
	network := renderNetwork(t)
	qps := renderQps(t)
	cc := renderConnectedClients(t)
	bc := renderBlockClient(t)
	memory := renderMemory(t)
	hp := renderKeyHitPercent(t)

	ui.Render(summary, network, qps, cc, memory, hp, bc)
}

func renderSummary(t *Top) *widgets.Paragraph {
	p := widgets.NewParagraph()
	p.Title = " Summary "
	uptime := timeToString(t.now.UptimeInSeconds)
	p.Text = fmt.Sprintf("Role:%s\nUptime:%s\nVersion:%s\nTotal Memory:%s\nConnected Client:%d\n",
		t.now.Role, uptime, t.now.Version, byteToString(int64(t.now.MaxMemery)), t.now.ConnectedClients,
	)
	p.SetRect(0, 0, 120, 10)
	p.TextStyle.Fg = ui.ColorMagenta

	return p
}

func renderMemory(t *Top) *widgets.Gauge {
	g := widgets.NewGauge()
	g.Title = " Memory "
	g.Percent = int(t.calMemory())
	height := 3
	mt := 20
	g.SetRect(60, mt, 120, mt+height)
	g.BarColor = ui.ColorGreen
	if g.Percent > 90 {
		g.BarColor = ui.ColorRed
	}
	g.TitleStyle.Fg = ui.ColorCyan

	return g
}

func renderKeyHitPercent(t *Top) *widgets.Gauge {
	g := widgets.NewGauge()
	g.Title = " Keyspace Hit Ratio "
	hp := math.Ceil(float64(t.now.KeyspaceHits) / (float64(t.now.KeyspaceHits + t.now.KeyspaceMisses)) * 100)
	if math.IsNaN(hp) {
		hp = 0
	}
	g.Percent = int(hp)
	height := 3
	mt := 23
	g.SetRect(60, mt, 120, mt+height)
	g.BarColor = ui.ColorGreen
	if g.Percent < 50 {
		g.BarColor = ui.ColorRed
	}
	g.TitleStyle.Fg = ui.ColorCyan

	return g
}

func renderBlockClient(t *Top) *widgets.SparklineGroup {
	sl := widgets.NewSparkline()
	sl.Title = fmt.Sprintf("%d", t.now.BlockedClients)
	sl.Data = t.now.BlockedClientsLine
	sl.LineColor = ui.ColorBlue

	slg := widgets.NewSparklineGroup(sl)
	slg.Title = " Blocked Clients "
	slg.TitleStyle.Fg = ui.ColorCyan
	slg.SetRect(60, 26, 120, 30)

	return slg
}

func renderQps(t *Top) *widgets.Plot {
	p := widgets.NewPlot()
	p.Title = " Qps "
	p.Data = make([][]float64, 1)
	sinData := (func() []float64 {
		ps := make([]float64, maxLen+1)
		copy(ps, t.now.QpsLine)
		return ps
	})()
	p.Data[0] = sinData
	p.SetRect(0, 20, 60, 10)
	p.AxesColor = ui.ColorWhite
	p.LineColors[0] = ui.ColorYellow

	return p
}

func renderConnectedClients(t *Top) *widgets.Plot {
	p := widgets.NewPlot()
	p.Title = " Connected Clients "
	p.Data = make([][]float64, 1)
	sinData := (func() []float64 {
		ps := make([]float64, maxLen+1)
		copy(ps, t.now.ConnectedClientLine)
		return ps
	})()
	p.Data[0] = sinData
	p.SetRect(60, 20, 120, 10)
	p.AxesColor = ui.ColorWhite
	p.LineColors[0] = ui.ColorYellow

	return p
}

func renderNetwork(t *Top) *widgets.SparklineGroup {
	sl := widgets.NewSparkline()
	sl.Title = fmt.Sprintf("In:%.2f Kb/s", t.now.InstantaneousInputKbps)
	sl.Data = t.now.InputKbsLine
	sl.LineColor = ui.ColorBlue

	sl2 := widgets.NewSparkline()
	sl2.Title = fmt.Sprintf("Out:%.2f Kb/s", t.now.InstantaneousOutputKbps)
	sl2.Data = t.now.OutputKbsLine
	sl2.LineColor = ui.ColorBlue

	slg := widgets.NewSparklineGroup(sl, sl2)
	slg.Title = " Network "
	slg.TitleStyle.Fg = ui.ColorCyan
	slg.SetRect(0, 30, 60, 20)

	return slg
}

func byteToString(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

const (
	secondsPerMinute = 60
	secondsPerHour   = 60 * secondsPerMinute
	secondsPerDay    = 24 * secondsPerHour
)

func timeToString(input int64) string {
	days := int(math.Floor(float64(input) / secondsPerDay))
	seconds := input % secondsPerDay
	hours := int(math.Floor(float64(seconds) / secondsPerHour))
	seconds = input % secondsPerHour
	minutes := int(math.Floor(float64(seconds) / secondsPerMinute))
	seconds = input % secondsPerMinute

	result := ""
	if days > 0 {
		result = fmt.Sprintf("%d days %d hours %d minutes %d seconds", days, hours, minutes, seconds)
	} else if hours > 0 {
		result = fmt.Sprintf("%d hours %d minutes %d seconds", hours, minutes, seconds)
	} else if minutes > 0 {
		result = fmt.Sprintf("%d minutes %d seconds", minutes, seconds)
	} else {
		result = fmt.Sprintf("%d seconds", seconds)
	}

	return result
}
