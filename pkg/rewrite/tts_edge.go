package rewrite

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/websocket"

	"github.com/pkg/errors"
)

const dialogueScriptMarker = "Followed by the actual dialogue script:"

const (
	edgeTTSWSSURL       = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1?TrustedClientToken=6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	edgeTTSOutputFormat = "audio-24khz-48kbitrate-mono-mp3"
	edgeTTSOrigin       = "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"
)

type edgeTTSSegment struct {
	voice string
	text  string
}

func edgeTTSMP3(ctx context.Context, transcript string, speakers []Speaker) (io.ReadCloser, error) {
	segments, err := buildEdgeTTSSegments(transcript, speakers)
	if err != nil {
		return nil, errors.Wrap(err, "build edge tts segments")
	}

	buf := bytes.NewBuffer(nil)
	for i, segment := range segments {
		audio, err := edgeTTSSingle(ctx, segment.text, segment.voice)
		if err != nil {
			return nil, errors.Wrapf(err, "synthesize edge tts segment %d", i)
		}
		if i > 0 {
			audio = stripLeadingID3Tag(audio)
		}
		if _, err := buf.Write(audio); err != nil {
			return nil, errors.Wrap(err, "write edge tts audio")
		}
	}

	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func buildEdgeTTSSegments(transcript string, speakers []Speaker) ([]edgeTTSSegment, error) {
	if len(speakers) == 0 {
		return nil, errors.New("no speakers")
	}

	script := transcript
	if idx := strings.Index(script, dialogueScriptMarker); idx >= 0 {
		script = script[idx+len(dialogueScriptMarker):]
	}
	script = strings.TrimSpace(script)
	if script == "" {
		return []edgeTTSSegment{{voice: speakers[0].Voice, text: transcript}}, nil
	}

	voiceBySpeaker := make(map[string]string, len(speakers))
	for _, speaker := range speakers {
		voiceBySpeaker[speaker.Name] = speaker.Voice
	}
	defaultVoice := speakers[0].Voice

	lines := strings.Split(script, "\n")
	segments := make([]edgeTTSSegment, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		speaker, text, ok := splitSpeakerLine(line)
		switch {
		case ok:
			voice := defaultVoice
			if v, exists := voiceBySpeaker[speaker]; exists {
				voice = v
			}
			segments = appendOrMergeSegment(segments, edgeTTSSegment{
				voice: voice,
				text:  text,
			})
		case len(segments) > 0:
			segments[len(segments)-1].text += "\n" + line
		default:
			segments = append(segments, edgeTTSSegment{
				voice: defaultVoice,
				text:  line,
			})
		}
	}

	if len(segments) == 0 {
		segments = append(segments, edgeTTSSegment{
			voice: defaultVoice,
			text:  script,
		})
	}

	return segments, nil
}

func splitSpeakerLine(line string) (speaker, text string, ok bool) {
	idx := strings.IndexAny(line, ":：")
	if idx <= 0 {
		return "", "", false
	}

	separator, _ := utf8.DecodeRuneInString(line[idx:])
	separatorLen := len(string(separator))
	speaker = strings.TrimSpace(line[:idx])
	text = strings.TrimSpace(line[idx+separatorLen:])

	if speaker == "" || text == "" {
		return "", "", false
	}

	return speaker, text, true
}

func appendOrMergeSegment(segments []edgeTTSSegment, segment edgeTTSSegment) []edgeTTSSegment {
	if len(segments) == 0 {
		return append(segments, segment)
	}
	last := &segments[len(segments)-1]
	if last.voice != segment.voice {
		return append(segments, segment)
	}
	last.text += "\n" + segment.text

	return segments
}

func edgeTTSSingle(ctx context.Context, text, voice string) ([]byte, error) {
	requestID, err := randomHex(16)
	if err != nil {
		return nil, errors.Wrap(err, "generate request id")
	}
	url := edgeTTSWSSURL + "&ConnectionId=" + requestID
	cfg, err := websocket.NewConfig(url, "https://edge.microsoft.com/")
	if err != nil {
		return nil, errors.Wrap(err, "create websocket config")
	}
	cfg.Header = http.Header{
		"Pragma":          []string{"no-cache"},
		"Cache-Control":   []string{"no-cache"},
		"Origin":          []string{edgeTTSOrigin},
		"Accept-Language": []string{"en-US,en;q=0.9"},
	}
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "connect edge tts websocket")
	}
	defer func() { _ = ws.Close() }()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = ws.Close()
		case <-done:
		}
	}()

	timestamp := edgeTTSTimestamp()
	speechConfig := buildEdgeSpeechConfigMessage(timestamp)
	if err := websocket.Message.Send(ws, speechConfig); err != nil {
		return nil, errors.Wrap(err, "send edge speech config")
	}
	ssmlMessage := buildEdgeSSMLMessage(requestID, timestamp, text, voice)
	if err := websocket.Message.Send(ws, ssmlMessage); err != nil {
		return nil, errors.Wrap(err, "send edge ssml")
	}

	audio := bytes.NewBuffer(nil)
	for {
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, errors.Wrap(err, "receive edge tts frame")
		}
		path, payload, err := parseEdgeFrame(frame)
		if err != nil {
			return nil, errors.Wrap(err, "parse edge tts frame")
		}
		switch path {
		case "audio":
			if _, err := audio.Write(payload); err != nil {
				return nil, errors.Wrap(err, "write edge tts chunk")
			}
		case "turn.end":
			goto doneReceive
		case "response":
			if bytes.Contains(frame, []byte("X-Error")) {
				return nil, errors.Errorf("edge tts response error: %s", strings.TrimSpace(string(frame)))
			}
		}
	}
doneReceive:

	if audio.Len() == 0 {
		return nil, errors.New("no audio data returned by edge tts")
	}

	return audio.Bytes(), nil
}

func stripLeadingID3Tag(data []byte) []byte {
	if len(data) < 10 || !bytes.HasPrefix(data, []byte("ID3")) {
		return data
	}

	size := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
	offset := 10 + size
	if offset >= len(data) {
		return data
	}

	return data[offset:]
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("size must be positive")
	}
	bs := make([]byte, size)
	if _, err := rand.Read(bs); err != nil {
		return "", errors.Wrap(err, "read random bytes")
	}

	return hex.EncodeToString(bs), nil
}

func edgeTTSTimestamp() string {
	// Matches the timestamp style used by Edge read-aloud clients.
	return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)")
}

func buildEdgeSpeechConfigMessage(timestamp string) string {
	return fmt.Sprintf("X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n{\"context\":{\"synthesis\":{\"audio\":{\"metadataoptions\":{\"sentenceBoundaryEnabled\":\"false\",\"wordBoundaryEnabled\":\"false\"},\"outputFormat\":\"%s\"}}}}", timestamp, edgeTTSOutputFormat)
}

func buildEdgeSSMLMessage(requestID, timestamp, text, voice string) string {
	ssml := "<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='en-US'>" +
		fmt.Sprintf("<voice name='%s'><prosody rate='+0%%' volume='+0%%'>", xmlEscape(voice)) +
		xmlEscape(text) +
		"</prosody></voice></speak>"

	return fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%s\r\nPath:ssml\r\n\r\n%s", requestID, timestamp, ssml)
}

func parseEdgeFrame(frame []byte) (path string, payload []byte, err error) {
	if len(frame) == 0 {
		return "", nil, nil
	}

	sep := []byte("\r\n\r\n")
	headerEnd := bytes.Index(frame, sep)
	if headerEnd < 0 {
		return "", frame, nil
	}

	header := string(frame[:headerEnd])
	payload = frame[headerEnd+len(sep):]

	switch {
	case strings.Contains(header, "Path:audio\r\n"), strings.HasSuffix(header, "Path:audio"):
		return "audio", payload, nil
	case strings.Contains(header, "Path:turn.end\r\n"), strings.HasSuffix(header, "Path:turn.end"):
		return "turn.end", payload, nil
	case strings.Contains(header, "Path:response\r\n"), strings.HasSuffix(header, "Path:response"):
		return "response", payload, nil
	default:
		return "", payload, nil
	}
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)

	return r.Replace(s)
}
