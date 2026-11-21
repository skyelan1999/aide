package voip

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// ServePhone dials a single SIP endpoint and exchanges audio for that dialog.
func ServePhone(ctx context.Context, target Target, input <-chan []byte, send func(Event) error) error {
	if err := target.Normalize(); err != nil {
		return err
	}
	phone := &net.UDPAddr{IP: net.ParseIP(target.IP), Port: int(target.Port)}
	probe, err := net.DialUDP("udp4", nil, phone)
	if err != nil {
		return errors.New("无法路由到话机地址")
	}
	local := probe.LocalAddr().(*net.UDPAddr).IP
	probe.Close()
	rtp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		return errors.New("无法开启话机音频端口")
	}
	defer rtp.Close()
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("Aide"))
	if err != nil {
		return err
	}
	defer ua.Close()
	signaling, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		return errors.New("无法开启 SIP 端口")
	}
	defer signaling.Close()
	client, err := sipgo.NewClient(ua, sipgo.WithClientAddr(signaling.LocalAddr().String()), sipgo.WithClientConnectionAddr(signaling.LocalAddr().String()), sipgo.WithClientNAT())
	if err != nil {
		return err
	}
	server, err := sipgo.NewServer(ua)
	if err != nil {
		return err
	}
	cache := sipgo.NewDialogClientCache(client, sip.ContactHeader{Address: sip.Uri{
		User: "aide", Host: local.String(), Port: signaling.LocalAddr().(*net.UDPAddr).Port,
	}})
	server.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		if cache.ReadBye(req, tx) != nil {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Call Does Not Exist", nil))
		}
	})
	ready := make(chan struct{})
	go func() {
		_ = server.ServeUDP(&sipSocket{UDPConn: signaling, phone: phone, ready: ready})
	}()
	select {
	case <-ctx.Done():
		return nil
	case <-ready:
	}
	offer := fmt.Sprintf("v=0\r\no=aide 1 1 IN IP4 %s\r\ns=Aide\r\nc=IN IP4 %s\r\nt=0 0\r\nm=audio %d RTP/AVP 0 8\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:8 PCMA/8000\r\na=ptime:20\r\na=sendrecv\r\n", local, local, rtp.LocalAddr().(*net.UDPAddr).Port)
	dialog, err := cache.Invite(ctx, sip.Uri{User: target.User, Host: target.IP, Port: int(target.Port)}, []byte(offer), sip.NewHeader("Content-Type", "application/sdp"))
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("无法向话机发送 SIP 呼叫")
	}
	defer dialog.Close()
	wait, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	var status int
	err = dialog.WaitAnswer(wait, sipgo.AnswerOptions{OnResponse: func(res *sip.Response) error {
		status = res.StatusCode
		if status == 180 || status == 183 {
			_ = send(Event{Op: "ringing", Code: status})
		}
		return nil
	}})
	if err != nil {
		// A final 2xx can race a local CANCEL. It still requires ACK and BYE.
		if dialog.InviteResponse == nil || !dialog.InviteResponse.IsSuccess() {
			if ctx.Err() != nil {
				return nil
			}
			if status >= 300 {
				return fmt.Errorf("话机拒绝 SIP 呼叫 (%d)", status)
			}
			return errors.New("话机接听超时，请检查地址和端口")
		}
		// The cancellation path yields the final response without initiating
		// media. ACK below so a simultaneous pickup can still be hung up.
		cancel()
	}
	answer := dialog.InviteResponse
	contact := answer.Contact()
	if contact == nil || contact.Address.Host != target.IP || (contact.Address.Port != int(target.Port) && (contact.Address.Port != 0 || target.Port != 5060)) || answer.GetHeader("Record-Route") != nil {
		return errors.New("话机返回了不同地址，无法直连")
	}
	sigCtx, stopSig := context.WithTimeout(context.Background(), 3*time.Second)
	err = dialog.Ack(sigCtx)
	stopSig()
	if err != nil {
		return errors.New("无法确认话机接听")
	}
	defer func() {
		if dialog.Context().Err() == nil {
			end, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			_ = dialog.Bye(end)
		}
	}()
	remote, codec, err := audioAnswer(string(answer.Body()), phone.IP)
	if err != nil {
		return err
	}
	if wait.Err() != nil || ctx.Err() != nil || send(Event{Op: "answered", Code: answer.StatusCode}) != nil {
		return nil
	}
	mediaCtx, stopMedia := context.WithCancel(ctx)
	defer stopMedia()
	go receiveRTP(mediaCtx, rtp, remote, codec, send)
	var seed [10]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return err
	}
	seq := binary.BigEndian.Uint16(seed[:2])
	stamp := binary.BigEndian.Uint32(seed[2:6])
	var packet [12 + PCMFrameBytes/2]byte
	packet[0] = 0x80
	packet[1] = codec
	copy(packet[8:12], seed[6:])
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-dialog.Context().Done():
			return nil
		case pcm := <-input:
			if len(pcm) != PCMFrameBytes {
				continue
			}
			for i := range packet[12:] {
				value := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
				if codec == 0 {
					packet[12+i] = ulaw(value)
				} else {
					packet[12+i] = alaw(value)
				}
			}
			binary.BigEndian.PutUint16(packet[2:4], seq)
			binary.BigEndian.PutUint32(packet[4:8], stamp)
			seq++
			stamp += PCMFrameBytes / 2
			if _, err := rtp.WriteToUDP(packet[:], remote); err != nil {
				return errors.New("无法向话机发送音频")
			}
		}
	}
}

type sipSocket struct {
	*net.UDPConn
	phone *net.UDPAddr
	ready chan struct{}
}

func (s *sipSocket) ReadFrom(b []byte) (int, net.Addr, error) {
	if s.ready != nil {
		close(s.ready)
		s.ready = nil
	}
	for {
		n, from, err := s.ReadFromUDP(b)
		if err != nil || (from.IP.Equal(s.phone.IP) && from.Port == s.phone.Port) {
			return n, from, err
		}
	}
}

func audioAnswer(sdp string, phone net.IP) (*net.UDPAddr, byte, error) {
	var sessionIP, mediaIP net.IP
	var port int
	var codec byte
	found, inAudio, supported, hold := false, false, false, false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "m="):
			inAudio = !found && strings.HasPrefix(line, "m=audio ")
			if !inAudio {
				continue
			}
			found = true
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[2] != "RTP/AVP" {
				break
			}
			port, _ = strconv.Atoi(fields[1])
			for _, value := range fields[3:] {
				if value == "0" || value == "8" {
					codec, supported = value[0]-'0', true
					break
				}
			}
		case strings.HasPrefix(line, "c=IN IP4 "):
			ip := net.ParseIP(strings.TrimSpace(strings.TrimPrefix(line, "c=IN IP4 ")))
			if !found {
				sessionIP = ip
			} else if inAudio {
				mediaIP = ip
			}
		case line == "a=inactive" || line == "a=sendonly" || line == "a=recvonly":
			if !found || inAudio {
				hold = true
			}
		}
	}
	if mediaIP == nil {
		mediaIP = sessionIP
	}
	if !supported || hold || port < 1 || port > 65535 || !mediaIP.Equal(phone) {
		return nil, 0, errors.New("话机未协商到同地址的双向 G.711 音频")
	}
	return &net.UDPAddr{IP: phone, Port: port}, codec, nil
}

func receiveRTP(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, codec byte, send func(Event) error) {
	packet := make([]byte, 2048)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, from, err := conn.ReadFromUDP(packet)
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				continue
			}
			return
		}
		if !from.IP.Equal(remote.IP) || from.Port != remote.Port {
			continue
		}
		data := rtpPayload(packet[:n], codec)
		if len(data) == 0 || len(data) > 800 {
			continue
		}
		pcm := make([]byte, len(data)*2)
		for i, b := range data {
			var sample int16
			if codec == 0 {
				sample = unUlaw(b)
			} else {
				sample = unAlaw(b)
			}
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
		}
		if send(Event{Op: "audio", PCM: pcm}) != nil {
			return
		}
	}
}

func rtpPayload(p []byte, codec byte) []byte {
	if len(p) < 12 || p[0]>>6 != 2 || p[1]&127 != codec {
		return nil
	}
	offset, size := 12+4*int(p[0]&15), len(p)
	if p[0]&16 != 0 {
		if offset+4 > size {
			return nil
		}
		offset += 4 + 4*int(binary.BigEndian.Uint16(p[offset+2:]))
	}
	if p[0]&32 != 0 {
		size -= int(p[size-1])
	}
	if offset >= size {
		return nil
	}
	return p[offset:size]
}
