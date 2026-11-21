package voip

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestG711RoundTrip(t *testing.T) {
	for _, pair := range []struct {
		encode func(int16) byte
		decode func(byte) int16
	}{{ulaw, unUlaw}, {alaw, unAlaw}} {
		for value := -32000; value <= 32000; value += 113 {
			result := int(pair.decode(pair.encode(int16(value))))
			if math.Abs(float64(result-value)) > 550 {
				t.Fatalf("G.711 conversion: %d -> %d", value, result)
			}
		}
	}
}

func TestBaresipDirectCall(t *testing.T) {
	if os.Getenv("AIDE_TEST_BARESIP") != "1" {
		t.Skip("set AIDE_TEST_BARESIP=1 in the Baresip test environment")
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var local string
	for _, addr := range addresses {
		if ip, ok := addr.(*net.IPNet); ok && ip.IP.To4() != nil && ip.IP.IsPrivate() {
			local = ip.IP.String()
			break
		}
	}
	if local == "" {
		t.Fatal("Baresip requires a local IPv4 interface")
	}

	for _, tc := range []struct {
		name, codec  string
		phoneHangup  bool
		cancelAtRing bool
	}{
		{"PCMU-phone-hangup", "pcmu", true, false},
		{"PCMA-aide-hangup", "pcma", false, false},
		{"cancel-before-answer", "pcmu", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testBaresipDialog(t, local, tc.codec, tc.phoneHangup, tc.cancelAtRing)
		})
	}
}

func testBaresipDialog(t *testing.T, local, codec string, phoneHangup, cancelAtRing bool) {
	t.Helper()
	dir := t.TempDir()
	// Distinct audio in each direction. The softphone uses a generated WAV as
	// its microphone and dumps the samples it decodes from the Aide RTP stream.
	source := filepath.Join(dir, "source.wav")
	if err := os.WriteFile(source, toneWAV(440, 4), 0600); err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	controlAddr := probe.Addr().String()
	probe.Close()
	sipProbe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(local)})
	if err != nil {
		t.Fatal(err)
	}
	sipPort := sipProbe.LocalAddr().(*net.UDPAddr).Port
	sipProbe.Close()
	config := fmt.Sprintf(`sip_listen %s:%d
call_local_timeout 10
audio_path /usr/share/baresip
audio_player aubridge,output
audio_alert aubridge,ring
audio_source aufile,%s
audio_txmode poll
audio_level no
module_path /usr/lib/baresip/modules
module g711.so
module aufile.so
module aubridge.so
module sndfile.so
module_app account.so
module_app menu.so
module_app ctrl_tcp.so
ctrl_tcp_listen %s
snd_path %s
`, local, sipPort, source, controlAddr, dir)
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	account := fmt.Sprintf("<sip:phone@%s:%d>;regint=0;answermode=manual;audio_codecs=%s/8000\n", local, sipPort, codec)
	if err := os.WriteFile(filepath.Join(dir, "accounts"), []byte(account), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 14*time.Second)
	defer stop()
	logPath := filepath.Join(dir, "baresip.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, "baresip", "-4", "-f", dir, "-s")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Log(string(data))
		}
	}()
	var control net.Conn
	for i := 0; i < 40; i++ {
		control, err = net.DialTimeout("tcp", controlAddr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(12 * time.Second))
	phoneEvents := bufio.NewReader(control)
	readEvent := func(kind string) {
		t.Helper()
		for {
			nText, err := phoneEvents.ReadString(':')
			if err != nil {
				t.Fatalf("Baresip event %s: %v", kind, err)
			}
			n, err := strconv.Atoi(nText[:len(nText)-1])
			if err != nil || n < 1 || n > 65536 {
				t.Fatalf("invalid Baresip message length %q", nText)
			}
			data := make([]byte, n+1)
			if _, err := io.ReadFull(phoneEvents, data); err != nil {
				t.Fatal(err)
			}
			var event struct{ Type string }
			if json.Unmarshal(data[:n], &event) == nil && event.Type == kind {
				return
			}
		}
	}
	command := func(name string) {
		t.Helper()
		body := fmt.Sprintf(`{"command":%q}`, name)
		if _, err := fmt.Fprintf(control, "%d:%s,", len(body), body); err != nil {
			t.Fatal(err)
		}
	}
	input := make(chan []byte)
	events := make(chan Event, 256)
	ended := make(chan error, 1)
	call, hangup := context.WithCancel(ctx)
	defer hangup()
	go func() {
		ended <- ServePhone(call, Target{IP: local, Port: uint16(sipPort), User: "phone"}, input, func(event Event) error {
			select {
			case events <- event:
				return nil
			case <-call.Done():
				return call.Err()
			}
		})
	}()
	readEvent("CALL_INCOMING")
	await := func(op string) Event {
		t.Helper()
		for {
			select {
			case e := <-events:
				if e.Op == op {
					return e
				}
			case err := <-ended:
				t.Fatalf("dialog ended waiting for %s: %v", op, err)
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s", op)
			}
		}
	}
	if got := await("ringing"); got.Code != 180 {
		t.Fatalf("ringing status %d", got.Code)
	}
	if !cancelAtRing {
		command("accept")
		if got := await("answered"); got.Code != 200 {
			t.Fatalf("answer status %d", got.Code)
		}
		readEvent("CALL_ESTABLISHED")
		go func() {
			data := toneWAV(660, 2)[44:]
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for offset := 0; offset < len(data); offset += PCMFrameBytes {
				select {
				case <-call.Done():
					return
				case <-ticker.C:
				}
				select {
				case <-call.Done():
					return
				case input <- data[offset : offset+PCMFrameBytes]:
				}
			}
		}()
		var received []byte
		for len(received) < 16000 {
			received = append(received, await("audio").PCM...)
		}
		assertTone(t, received, 440, 660)
	}

	if phoneHangup {
		command("hangup")
	} else {
		hangup()
	}
	readEvent("CALL_CLOSED")
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("dialog did not close")
	}
	if cancelAtRing {
		return
	}
	files, err := filepath.Glob(filepath.Join(dir, "*-dec.wav"))
	if err != nil || len(files) != 1 {
		t.Fatalf("Baresip receiver recording: %v %v", files, err)
	}
	recording, err := os.ReadFile(files[0])
	if err != nil || len(recording) < 1600 {
		t.Fatalf("Baresip receiver recording: %d bytes, %v", len(recording), err)
	}
	// WAV PCM data follows a data chunk, which may follow additional metadata.
	for i := 12; i+8 <= len(recording); {
		n := int(binary.LittleEndian.Uint32(recording[i+4:]))
		if i+8+n > len(recording) {
			t.Fatal("incomplete Baresip WAV recording")
		}
		if string(recording[i:i+4]) == "data" {
			assertTone(t, recording[i+8:i+8+n], 660, 440)
			return
		}
		i += 8 + n + n%2
	}
	t.Fatal("Baresip WAV has no audio")
}

func toneWAV(frequency, seconds int) []byte {
	const rate = 8000
	out := make([]byte, 44+2*rate*seconds)
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], rate)
	binary.LittleEndian.PutUint32(out[28:], 2*rate)
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(out)-44))
	for i := 0; i < rate*seconds; i++ {
		sample := int16(10000 * math.Sin(float64(i*frequency)*2*math.Pi/rate))
		binary.LittleEndian.PutUint16(out[44+i*2:], uint16(sample))
	}
	return out
}

func assertTone(t *testing.T, pcm []byte, want, other int) {
	t.Helper()
	power := func(freq int) float64 {
		var real, imag float64
		for i := 0; 2*i+2 <= len(pcm); i++ {
			angle := 2 * math.Pi * float64(freq*i) / 8000
			s := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
			real += s * math.Cos(angle)
			imag += s * math.Sin(angle)
		}
		return math.Hypot(real, imag) / float64(len(pcm)/2)
	}
	p, q := power(want), power(other)
	if p < 500 || p < q*5 {
		t.Fatalf("expected audible %d Hz audio, levels %.1f at %d Hz and %.1f at %d Hz", want, p, want, q, other)
	}
	t.Logf("decoded %d Hz (level %.0f), independent of the opposite direction", want, p)
}
