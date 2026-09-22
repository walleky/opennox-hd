package opennox

import (
	"fmt"
	"image"
	"time"

	"github.com/opennox/libs/common"
	"github.com/opennox/libs/log"
	"github.com/opennox/libs/platform"

	noxflags "github.com/opennox/opennox/v1/common/flags"
	"github.com/opennox/opennox/v1/common/memmap"
	"github.com/opennox/opennox/v1/internal/netlist"
	"github.com/opennox/opennox/v1/server"
	"github.com/opennox/opennox/v1/server/netlib"
)

var (
	noxPerfmon = newPerfmon()
	bandLog    = log.New("bandwidth")
)

func newPerfmon() *Perfmon {
	return &Perfmon{nextCnt: 10, logger: bandLog}
}

type Perfmon struct {
	enabled       bool
	nextCnt       uint
	cnt           uint
	prevTicks     time.Duration
	transfer      [common.MaxPlayers]uint32
	transferTick  [common.MaxPlayers]time.Duration
	packetSizeCli int
	latePackets   int

	logger           *log.Logger
	loggerHdr        bool
	logBandLast      time.Duration
	hdLogLast        time.Duration
	hdShutdownLogged bool
	hdTraceArmed     bool

	bandInd     int
	bandHistory [128]int

	ping        time.Duration
	pingInd     int
	pingHistory [128]int

	profInd int

	profClient     time.Duration
	profClientHist [128]int

	profServer     time.Duration
	profServerHist [128]int

	fps        int
	fpsInd     int
	fpsHistory [128]int
}

func (m *Perfmon) Toggle() {
	m.enabled = !m.enabled
}

// LogHD emits one compact, once-per-second snapshot. The renderer owns the
// counters and is single-threaded with the draw loop, so no synchronization is
// needed here.
func (m *Perfmon) LogHD(c *Client) {
	if m == nil || c == nil || c.r == nil {
		return
	}
	ticks := platform.Ticks()
	if ticks-m.hdLogLast <= time.Second {
		return
	}
	m.hdLogLast = ticks
	hs := c.r.TextureDensityMetrics()
	im := c.Inp.InputMetrics()
	if c.Win != nil {
		hw := c.Win.Metrics()
		hs.UploadTime = hw.UploadTime
		hs.SurfaceRecreations = hw.SurfaceRecreations
	}
	m.logger.Printf("[hdperf] frames=%d avg=%s p95=%s max=%s decode=%d/%d/%s cache=%d/%d evict=%d expand=%s replay=%s upload=%s q=%d/%d reject=%d reasons=%v surfaces=%d fonts=%d input=mouse:%d/%d/%s/%s key:%d/%d/%s/%s\n",
		hs.Frames, hs.FrameAverage, hs.FrameP95, hs.FrameMax, hs.DecodeMisses, hs.DecodeFailures, hs.DecodeTime,
		hs.CacheBytes, hs.CacheHighWater, hs.CacheEvictions, hs.ExpansionTime, hs.ReplayTime, hs.UploadTime,
		hs.QueueCount, hs.QueueBytes, hs.QueueRejected, hs.QueueRejects, hs.SurfaceRecreations, hs.FontFallbacks,
		im.MouseDrops, im.MouseAgeCount, im.MouseAgeAverage, im.MouseAgeMax, im.KeyDrops, im.KeyAgeCount, im.KeyAgeAverage, im.KeyAgeMax)
}

func (m *Perfmon) LogHDShutdown(c *Client) {
	if m == nil || !m.enabled || m.hdShutdownLogged {
		return
	}
	m.hdShutdownLogged = true
	m.hdLogLast = 0
	m.LogHD(c)
}

func (m *Perfmon) HDOverlay(c *Client, x, y int) {
	if m == nil || c == nil || c.r == nil {
		return
	}
	hs := c.r.TextureDensityMetrics()
	im := c.Inp.InputMetrics()
	c.r.Data().SetTextColor(nox_color_white_2523948)
	c.r.DrawString(nil, fmt.Sprintf("HD q:%d/%d r:%d f:%d c:%d in:%d/%d", hs.QueueCount, hs.QueueBytes, hs.QueueRejected, hs.FontFallbacks, hs.CacheEvictions, im.MouseDrops, im.KeyDrops), image.Pt(x, y))
}

func (m *Perfmon) LogBandwidth(s *server.Server, nets netlib.Streams) {
	ticks := platform.Ticks()
	if ticks-m.logBandLast <= time.Second {
		return
	}
	m.logBandLast = ticks

	if !m.loggerHdr {
		m.loggerHdr = true
		m.logger.Print("Player,\tBPS, Frame, Threshold, Resend Interval, Resends Per Update, Sleep Interval\n\n")
	}
	m.logger.Print("\n")
	for _, pl := range s.Players.List() {
		d := m.bandData(pl.Index())
		v4 := s.Frame()
		var bps uint32
		if pl.Index() == server.HostPlayerIndex {
			bps = m.TransferStats(nets.HostStream())
		} else {
			bps = m.TransferStats(nets.StreamByPlayerInd(pl.PlayerIndex()))
		}
		m.logger.Printf("%s, %d, %d, %d, %d, %d\n", pl.Name(), bps, v4, d.th, d.ri, d.rpu)
	}
}

type playerBandData struct {
	rpu, ri, th uint32
}

func (m *Perfmon) bandData(ind int) playerBandData {
	arr := memmap.PtrT[[3 * common.MaxPlayers]uint32](0x5D4594, 1565124)[:]
	arr = arr[3*ind : 3*(ind+1)]
	return playerBandData{
		rpu: arr[0] & 0xff,
		ri:  (arr[0] >> 8) & 0xff,
		th:  arr[1],
	}
}

func (m *Perfmon) TransferStats(conn netlib.StreamStats) uint32 {
	ticks := platform.Ticks()
	var ri int
	if conn.IsHost() {
		ri = 0
	} else {
		ri = int(conn.Player()) - 1
	}
	prev := m.transferTick[ri]
	if ticks < prev+time.Second {
		return m.transfer[ri]
	}
	m.transferTick[ri] = ticks
	stat := conn.TransferStats()
	m.transfer[ri] = stat
	return stat
}

func (m *Perfmon) packetSize(l *netlist.List) int {
	if !noxflags.HasGame(noxflags.GameHost) {
		return m.packetSizeCli
	}
	return l.ByInd(server.HostPlayerIndex, netlist.Kind1).Size() + l.ByInd(server.HostPlayerIndex, netlist.Kind2).Size()
}

func (m *Perfmon) startProfileClient() func() {
	start := platform.Ticks()
	return func() {
		m.profClient = platform.Ticks() - start
	}
}

func (m *Perfmon) startProfileServer() func() {
	start := platform.Ticks()
	return func() {
		m.profServer = platform.Ticks() - start
	}
}
