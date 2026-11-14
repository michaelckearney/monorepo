//go:build linux

package securitykey

import (
	"os"
	"path/filepath"
)

// Devices are the security keys plugged in. On Linux each is a hidraw
// device; the user needs access to it, which distributions give for FIDO
// keys (systemd's uaccess).
func Devices() ([]Device, error) {
	descriptors, err := filepath.Glob("/sys/class/hidraw/hidraw*/device/report_descriptor")
	if err != nil {
		return nil, err
	}
	var devs []Device
	for _, path := range descriptors {
		descriptor, err := os.ReadFile(path)
		if err != nil || !isFIDO(descriptor) {
			continue
		}
		name := filepath.Base(filepath.Dir(filepath.Dir(path)))
		f, err := os.OpenFile("/dev/"+name, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		d := &hidraw{f: f, reports: newReports()}
		go d.pump()
		devs = append(devs, d)
	}
	return devs, nil
}

type hidraw struct {
	f *os.File
	*reports
}

func (d *hidraw) pump() {
	for {
		buf := make([]byte, reportSize)
		n, err := d.f.Read(buf)
		if err != nil {
			d.stop(err)
			return
		}
		d.put(buf[:n])
	}
}

// Write sends a report, after its report number, which FIDO keys leave 0.
func (d *hidraw) Write(report []byte) error {
	_, err := d.f.Write(append([]byte{0}, report...))
	return err
}

func (d *hidraw) Close() error {
	d.stop(nil)
	return d.f.Close()
}
