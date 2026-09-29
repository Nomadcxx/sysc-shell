package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// DDC/CI over I2C, following the framing DankMaterialShell's brightness ddc.go
// uses: a write packet is [0x51, len|0x80, payload..., checksum] with the
// checksum an XOR of every byte against 0x6E. Replies arrive on slave 0x6E.
// Joining a bus to a connector needs no adapter-name heuristic: the EDID read
// over the bus is byte-identical to /sys/class/drm/<connector>/edid.
const (
	ioctlI2CSlave    = 0x0703
	ioctlI2CRDWR     = 0x0707
	ddcSlaveAddr     = 0x37
	edidSlaveAddr    = 0x50
	ddcSourceAddr    = 0x51
	ddcReplyAddr     = 0x6e
	ddcOpGet         = 0x01
	ddcOpSet         = 0x03
	ddcVCPBright     = 0x10
	ddcVCPCap        = 0xF3
	ddcChecksumKey   = 0x6e
	ddcSettle        = 50 * time.Millisecond
	ddcReplyTimeout  = 200 * time.Millisecond
	ddcCapabilityTry = 3
	ddcRetryPause    = 100 * time.Millisecond
	edidLength       = 128
)

func ddcciChecksum(payload []byte) byte {
	var x byte = ddcChecksumKey
	for _, b := range payload {
		x ^= b
	}
	return x
}

// ddcWriteFrame builds the i2c write payload: header, body, checksum.
func ddcWriteFrame(op byte, body ...byte) []byte {
	data := append([]byte{op}, body...)
	frame := append([]byte{ddcSourceAddr, byte(len(data)) | 0x80}, data...)
	return append(frame, ddcciChecksum(frame))
}

// parseVCPReply decodes a get_vcp_feature reply:
// [0x6E, 0x9F, len, result, op, vcp, type, maxHi, maxLo, curHi, curLo, cks].
func parseVCPReply(buf []byte, vcp byte) (current, max int, err error) {
	if len(buf) < 10 {
		return 0, 0, fmt.Errorf("ddc: short reply (%d bytes)", len(buf))
	}
	if len(buf) < 11 {
		return 0, 0, fmt.Errorf("ddc: short reply (%d bytes)", len(buf))
	}
	if buf[0] != ddcReplyAddr {
		return 0, 0, fmt.Errorf("ddc: bad reply address 0x%02x", buf[0])
	}
	if buf[2] != 0x02 {
		return 0, 0, fmt.Errorf("ddc: bad reply length 0x%02x", buf[2])
	}
	if buf[3] != 0x00 {
		return 0, 0, fmt.Errorf("ddc: feature 0x%02x not supported (result 0x%02x)", vcp, buf[3])
	}
	if buf[4] != vcp {
		return 0, 0, fmt.Errorf("ddc: reply echoes VCP 0x%02x, want 0x%02x", buf[4], vcp)
	}
	max = int(buf[6])<<8 | int(buf[7])
	current = int(buf[8])<<8 | int(buf[9])
	return current, max, nil
}

// ignorableAdapterName rejects buses no display can hang off, mirroring the
// filter DMS keeps for iGPU/SMBus controllers.
func ignorableAdapterName(name string) bool {
	for _, p := range []string{"SMBus", "Synopsys DesignWare", "soc:i2cdsi", "smu", "mac-io", "u4"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	if strings.HasPrefix(name, "nouveau") && !strings.HasPrefix(name, "nvkm-") {
		return true
	}
	return false
}

// matchEDIDToConnector returns the connector name (DP-1, eDP-1...) whose
// sysfs EDID equals the one read over the bus, or "" when no connector has it.
func matchEDIDToConnector(edid []byte, drmRoot string) string {
	if len(edid) < edidLength {
		return ""
	}
	dirs, err := os.ReadDir(drmRoot)
	if err != nil {
		return ""
	}
	for _, d := range dirs {
		name := d.Name()
		if !strings.HasPrefix(name, "card") || strings.HasPrefix(name, "card-") {
			continue
		}
		file, err := os.ReadFile(filepath.Join(drmRoot, name, "edid"))
		if err != nil || len(file) < edidLength {
			continue
		}
		if bytes.Equal(file[:edidLength], edid[:edidLength]) {
			if idx := strings.Index(name, "-"); idx >= 0 {
				return name[idx+1:]
			}
			return name
		}
	}
	return ""
}

type i2cMsg struct {
	addr  uint16
	flags uint16
	len   uint16
	_     uint16
	ptr   uint64
}

// i2cRdwrData mirrors struct i2c_rdwr_ioctl_data: the I2C_RDWR ioctl takes a
// pointer to this header, not to the message array itself.
type i2cRdwrData struct {
	msgs  *i2cMsg
	nmsgs uint32
	_     uint32
}

func i2cRDWR(fd int, msgs []i2cMsg) error {
	data := i2cRdwrData{msgs: &msgs[0], nmsgs: uint32(len(msgs))}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(ioctlI2CRDWR),
		uintptr(unsafe.Pointer(&data)))
	if errno != 0 {
		return errno
	}
	return nil
}

// readBusEDID performs a plain-offset zero read of block 0, the same transfer
// pattern proven on this machine's NVIDIA adapter.
func readBusEDID(fd int) ([]byte, error) {
	buf := make([]byte, edidLength)
	var offset byte
	msgs := []i2cMsg{
		{addr: edidSlaveAddr, flags: 0, len: 1, ptr: uint64(uintptr(unsafe.Pointer(&offset)))},
		{addr: edidSlaveAddr, flags: 1, len: edidLength, ptr: uint64(uintptr(unsafe.Pointer(&buf[0])))},
	}
	if err := i2cRDWR(fd, msgs); err != nil {
		return nil, err
	}
	return buf, nil
}

func ddcSetSlave(fd int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(ioctlI2CSlave), uintptr(ddcSlaveAddr))
	if errno != 0 {
		return errno
	}
	return nil
}

// ddcFlush drops up to 3 replies a previous reader left pending.
func ddcFlush(fd int) {
	buf := make([]byte, 32)
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		n, _ := syscall.Read(fd, buf)
		if n <= 0 {
			return
		}
	}
}

func ddcGetVCP(fd int, vcp byte) (current, max int, err error) {
	ddcFlush(fd)
	frame := ddcWriteFrame(ddcOpGet, vcp)
	if _, err = syscall.Write(fd, frame); err != nil {
		return 0, 0, err
	}
	time.Sleep(ddcSettle)
	pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if _, err = unix.Poll(pfds, int(ddcReplyTimeout.Milliseconds())); err != nil {
		return 0, 0, err
	}
	if pfds[0].Revents&unix.POLLIN == 0 {
		return 0, 0, fmt.Errorf("ddc: no reply within %v", ddcReplyTimeout)
	}
	buf := make([]byte, 12)
	n, err := syscall.Read(fd, buf)
	if err != nil {
		return 0, 0, err
	}
	return parseVCPReply(buf[:n], vcp)
}

func ddcSetVCP(fd int, vcp byte, value int) error {
	if err := ddcSetSlave(fd); err != nil {
		return err
	}
	frame := ddcWriteFrame(ddcOpSet, vcp, byte(value>>8), byte(value))
	if _, err := syscall.Write(fd, frame); err != nil {
		return err
	}
	time.Sleep(ddcSettle)
	return nil
}

// probeDDCBus returns the connector behind /dev/i2c-<bus> and the brightness
// feature's maximum. Any failure means "no DDC-capable display here": the
// caller treats it as absent, never as an error to retry in a loop.
func probeDDCBus(bus int, devRoot, drmRoot, i2cSysfsRoot string) (string, int, error) {
	nameFile, err := os.ReadFile(filepath.Join(i2cSysfsRoot, fmt.Sprintf("i2c-%d", bus), "name"))
	if err == nil && ignorableAdapterName(strings.TrimSpace(string(nameFile))) {
		return "", 0, fmt.Errorf("ignorable adapter")
	}
	fd, err := syscall.Open(filepath.Join(devRoot, fmt.Sprintf("i2c-%d", bus)), syscall.O_RDWR, 0)
	if err != nil {
		return "", 0, err
	}
	defer syscall.Close(fd)
	edid, err := readBusEDID(fd)
	if err != nil {
		return "", 0, err
	}
	connector := matchEDIDToConnector(edid, drmRoot)
	if connector == "" {
		return "", 0, fmt.Errorf("no connector matches this bus's EDID")
	}
	if err := ddcSetSlave(fd); err != nil {
		return "", 0, err
	}
	var max int
	for i := 0; i < ddcCapabilityTry; i++ {
		_, max, err = ddcGetVCP(fd, ddcVCPBright)
		if err == nil && max > 0 {
			break
		}
		time.Sleep(ddcRetryPause)
	}
	if err != nil || max == 0 {
		return "", 0, fmt.Errorf("brightness feature unreadable: %w", err)
	}
	return connector, max, nil
}

func ddcPercentToValue(percent, max int) int {
	if percent < 1 {
		percent = 1
	}
	if percent > 100 {
		percent = 100
	}
	return 1 + (percent-1)*(max-1)/99
}

func ddcValueToPercent(value, max int) int {
	if max <= 1 {
		return 100
	}
	if value < 1 {
		value = 1
	}
	return 1 + (value-1)*99/(max-1)
}
