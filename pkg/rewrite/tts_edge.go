package rewrite

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/difyz9/edge-tts-go/pkg/communicate"
	"github.com/pkg/errors"
)

const dialogueScriptMarker = "Followed by the actual dialogue script:"

const (
	edgeTTSRate                  = "+0%"
	edgeTTSVolume                = "+0%"
	edgeTTSPitch                 = "+0Hz"
	edgeTTSConnectTimeoutSeconds = 10
	edgeTTSReceiveTimeoutSeconds = 60
	edgeTTSRetryAttempts         = 3
	edgeTTSRetryDelay            = 500 * time.Millisecond
	edgeTTSErrorSnippetRunes     = 120
)

type edgeTTSSegment struct {
	voice string
	text  string
}

var newEdgeTTSCommunicate = communicate.NewCommunicate

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
	var lastErr error
	for attempt := 1; attempt <= edgeTTSRetryAttempts; attempt++ {
		audio, err := edgeTTSSingleAttempt(ctx, text, voice)
		if err == nil {
			return audio, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
		if attempt == edgeTTSRetryAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "edge tts context done")
		case <-time.After(edgeTTSRetryDelay):
		}
	}

	return nil, errors.Wrapf(lastErr, "voice=%s text=%q", voice, edgeTTSErrorSnippet(text))
}

func edgeTTSSingleAttempt(ctx context.Context, text, voice string) ([]byte, error) {
	comm, err := newEdgeTTSCommunicate(
		text,
		voice,
		edgeTTSRate,
		edgeTTSVolume,
		edgeTTSPitch,
		"",
		edgeTTSConnectTimeoutSeconds,
		edgeTTSReceiveTimeoutSeconds,
	)
	if err != nil {
		return nil, errors.Wrap(err, "create edge-tts-go communicator")
	}

	chunkChan, errChan := comm.Stream(ctx)
	audio := bytes.NewBuffer(nil)
	for chunk := range chunkChan {
		if chunk.Type != "audio" {
			continue
		}
		if _, err := audio.Write(chunk.Data); err != nil {
			return nil, errors.Wrap(err, "write edge tts chunk")
		}
	}

	if err := <-errChan; err != nil {
		return nil, errors.Wrap(err, "stream edge tts audio")
	}
	if audio.Len() == 0 {
		return nil, errors.New("no audio data returned by edge tts")
	}

	return audio.Bytes(), nil
}

func edgeTTSErrorSnippet(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= edgeTTSErrorSnippetRunes {
		return text
	}

	return string(runes[:edgeTTSErrorSnippetRunes]) + "..."
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
