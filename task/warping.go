package task

import (
	"CloudflareWarpSpeedTest/utils"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	defaultRoutines   = 200
	defaultPingTimes  = 20
	udpConnectTimeout = time.Millisecond * 500

	warpValidatePacket = "cf000000628748824150e38f5c64b477"
)

var (
	ScanAllPort = false

	warpPorts = []int{890, 891}

	Routines = defaultRoutines

	PingTimes int = defaultPingTimes

	MaxWarpPortRange = 1000

	warpHandshakePacket, _ = hex.DecodeString("04e77a11628748824150e38f5c64b4776d82d118ed6ee00d8ede7ae82405df0c380000000000000000000000004154e7e7b6bbbb84ab8cd5e9b0f82a1c")
)

type UDPAddr struct {
	IP   *net.IPAddr
	Port int
}

type Warping struct {
	wg      *sync.WaitGroup
	m       *sync.Mutex
	ips     []*UDPAddr
	csv     utils.PingDelaySet
	control chan bool
	bar     *utils.Bar
}

func NewWarping() *Warping {
	checkPingDefault()
	ips := loadWarpIPRanges()
	return &Warping{
		wg:      &sync.WaitGroup{},
		m:       &sync.Mutex{},
		ips:     ips,
		csv:     make(utils.PingDelaySet, 0),
		control: make(chan bool, Routines),
		bar:     utils.NewBar(len(ips), "可用:", ""),
	}
}

func checkPingDefault() {
	if Routines <= 0 {
		Routines = defaultRoutines
	}
	if PingTimes <= 0 {
		PingTimes = defaultPingTimes
	}
}

func (w *Warping) Run() utils.PingDelaySet {
	if len(w.ips) == 0 {
		return w.csv
	}
	for _, ip := range w.ips {
		w.wg.Add(1)
		w.control <- false
		go w.start(ip)
	}
	w.wg.Wait()
	w.bar.Done()
	sort.Sort(w.csv)
	return w.csv
}

func (w *Warping) start(ip *UDPAddr) {
	defer w.wg.Done()
	w.warpingHandler(ip)
	<-w.control
}

func (w *Warping) checkConnection(ip *UDPAddr) (recv int, totalDelay time.Duration) {
	for i := 0; i < PingTimes; i++ {
		if ok, delay := w.warping(ip); ok {
			recv++
			totalDelay += delay
		}
	}
	return
}

func (w *Warping) warpingHandler(ip *UDPAddr) {
	recv, totalDelay := w.checkConnection(ip)
	nowAble := len(w.csv)
	if recv != 0 {
		nowAble++
	}
	w.bar.Grow(1, strconv.Itoa(nowAble))
	if recv == 0 {
		return
	}
	data := &utils.PingData{
		IP:       ip.ToUDPAddr(),
		Sended:   PingTimes,
		Received: recv,
		Delay:    totalDelay / time.Duration(recv),
	}
	w.appendIPData(data)
}

func (w *Warping) appendIPData(data *utils.PingData) {
	w.m.Lock()
	defer w.m.Unlock()
	w.csv = append(w.csv, utils.CloudflareIPData{
		PingData: data,
	})
}

func loadWarpIPRanges() (ipAddrs []*UDPAddr) {
	ips := loadIPRanges()
	for _, ip := range ips {
		portAddrs := generateIPAddrWithPorts(ip)
		ipAddrs = append(ipAddrs, portAddrs...)
	}
	return ipAddrs
}

func generateIPAddrWithPorts(ip *net.IPAddr) (udpAddrs []*UDPAddr) {
	if !ScanAllPort {
		for _, port := range warpPorts {
			udpAddrs = append(udpAddrs, &UDPAddr{
				IP:   ip,
				Port: port,
			})
		}
		return
	}
	for port := 1; port <= MaxWarpPortRange; port++ {
		udpAddrs = append(udpAddrs, &UDPAddr{
			IP:   ip,
			Port: port,
		})
	}
	return udpAddrs
}

func (i *UDPAddr) FullAddress() string {
	if isIPv4(i.IP.String()) {
		return fmt.Sprintf("%s:%d", i.IP.String(), i.Port)
	}
	return fmt.Sprintf("[%s]:%d", i.IP.String(), i.Port)

}

func (i *UDPAddr) ToUDPAddr() (addr *net.UDPAddr) {
	addr, _ = net.ResolveUDPAddr("udp", i.FullAddress())
	return
}

func (w *Warping) warping(ip *UDPAddr) (bool, time.Duration) {

	fullAddress := ip.FullAddress()
	conn, err := net.DialTimeout("udp", fullAddress, udpConnectTimeout)
	if err != nil {
		return false, 0
	}
	defer conn.Close()
	startTime := time.Now()

	_, err = conn.Write(warpHandshakePacket)
	if err != nil {
		return false, 0
	}

	revBuff := make([]byte, 1024)

	err = conn.SetDeadline(time.Now().Add(time.Second))
	if err != nil {
		return false, 0
	}
	n, err := conn.Read(revBuff)
	if err != nil {
		return false, 0
	}
	handshakeResponse := hex.EncodeToString(revBuff[:n])
	if handshakeResponse != warpValidatePacket {
		return false, 0
	}

	duration := time.Since(startTime)
	return true, duration
}