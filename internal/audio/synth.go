// Package audio provides audio synthesis for MIDI playback
package audio

import (
	"math"
	"sync"

	"github.com/ebitengine/oto/v3"
)

const (
	sampleRate   = 44100
	channelCount = 2 // stereo
	bitDepth     = 2 // 16-bit
)

// WaveType represents different oscillator wave shapes
type WaveType int

const (
	WaveSine WaveType = iota
	WaveSquare
	WaveSawtooth
	WaveTriangle
)

// Voice represents a single playing note
type Voice struct {
	note      uint8
	channel   uint8
	velocity  uint8
	frequency float64
	phase     float64
	envelope  float64 // 0-1 for ADSR envelope
	releasing bool
	active    bool
}

// Synth is a polyphonic synthesizer
type Synth struct {
	mu           sync.RWMutex
	otoCtx       *oto.Context
	player       *oto.Player
	voices       []*Voice
	maxVoices    int
	masterVolume float64
	waveTypes    [16]WaveType // wave type per MIDI channel
	running      bool
}

// NewSynth creates a new synthesizer
func NewSynth() (*Synth, error) {
	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channelCount,
		Format:       oto.FormatSignedInt16LE,
	}

	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return nil, err
	}
	<-readyChan

	s := &Synth{
		otoCtx:       otoCtx,
		maxVoices:    64,
		masterVolume: 0.3,
		running:      true,
	}

	// Assign different wave types to channels for variety
	s.waveTypes[0] = WaveSine     // Piano-ish
	s.waveTypes[1] = WaveTriangle // Soft lead
	s.waveTypes[2] = WaveSawtooth // Bright lead
	s.waveTypes[3] = WaveSquare   // Retro/8-bit
	for i := 4; i < 16; i++ {
		s.waveTypes[i] = WaveSine
	}

	// Start the audio stream
	s.player = otoCtx.NewPlayer(&synthReader{synth: s})
	s.player.Play()

	return s, nil
}

// synthReader implements io.Reader for continuous audio generation
type synthReader struct {
	synth *Synth
}

func (r *synthReader) Read(buf []byte) (int, error) {
	s := r.synth
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate samples
	numSamples := len(buf) / (channelCount * bitDepth)

	for i := 0; i < numSamples; i++ {
		var sample float64

		// Mix all active voices
		for _, v := range s.voices {
			if v == nil || !v.active {
				continue
			}

			// Generate waveform based on channel's wave type
			waveType := s.waveTypes[v.channel%16]
			oscSample := generateWave(waveType, v.phase)

			// Apply velocity and envelope
			velocityScale := float64(v.velocity) / 127.0
			sample += oscSample * velocityScale * v.envelope * 0.2

			// Advance phase
			v.phase += v.frequency / sampleRate
			if v.phase >= 1.0 {
				v.phase -= 1.0
			}

			// Update envelope
			if v.releasing {
				// Release phase - exponential decay
				v.envelope *= 0.9995
				if v.envelope < 0.001 {
					v.active = false
				}
			} else if v.envelope < 1.0 {
				// Attack phase
				v.envelope += 0.001
				if v.envelope > 1.0 {
					v.envelope = 1.0
				}
			}
		}

		// Apply master volume and clip
		sample *= s.masterVolume
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}

		// Convert to 16-bit signed integer
		sampleInt := int16(sample * 32767)

		// Write stereo samples (same for L and R)
		idx := i * channelCount * bitDepth
		buf[idx] = byte(sampleInt)
		buf[idx+1] = byte(sampleInt >> 8)
		buf[idx+2] = byte(sampleInt)
		buf[idx+3] = byte(sampleInt >> 8)
	}

	return len(buf), nil
}

func generateWave(waveType WaveType, phase float64) float64 {
	switch waveType {
	case WaveSine:
		return math.Sin(2 * math.Pi * phase)
	case WaveSquare:
		if phase < 0.5 {
			return 0.8
		}
		return -0.8
	case WaveSawtooth:
		return 2*phase - 1
	case WaveTriangle:
		if phase < 0.5 {
			return 4*phase - 1
		}
		return 3 - 4*phase
	default:
		return math.Sin(2 * math.Pi * phase)
	}
}

// NoteOn triggers a new note
func (s *Synth) NoteOn(channel, note, velocity uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if velocity == 0 {
		s.noteOffLocked(channel, note)
		return
	}

	// Find an inactive voice or steal the oldest one
	var voice *Voice
	for _, v := range s.voices {
		if v != nil && !v.active {
			voice = v
			break
		}
	}

	if voice == nil {
		if len(s.voices) < s.maxVoices {
			voice = &Voice{}
			s.voices = append(s.voices, voice)
		} else {
			// Steal oldest voice
			voice = s.voices[0]
		}
	}

	voice.note = note
	voice.channel = channel
	voice.velocity = velocity
	voice.frequency = midiNoteToFreq(note)
	voice.phase = 0
	voice.envelope = 0
	voice.releasing = false
	voice.active = true
}

// NoteOff releases a note
func (s *Synth) NoteOff(channel, note uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteOffLocked(channel, note)
}

func (s *Synth) noteOffLocked(channel, note uint8) {
	for _, v := range s.voices {
		if v != nil && v.active && v.note == note && v.channel == channel && !v.releasing {
			v.releasing = true
			break
		}
	}
}

// AllNotesOff stops all playing notes
func (s *Synth) AllNotesOff() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range s.voices {
		if v != nil && v.active {
			v.releasing = true
		}
	}
}

// SetVolume sets the master volume (0.0 - 1.0)
func (s *Synth) SetVolume(vol float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}
	s.masterVolume = vol
}

// Close shuts down the synthesizer
func (s *Synth) Close() error {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	// Note: As of oto v3.4, player.Close() is deprecated and no longer needed.
	// The player will be cleaned up when garbage collected.
	return nil
}

// midiNoteToFreq converts a MIDI note number to frequency in Hz
func midiNoteToFreq(note uint8) float64 {
	// A4 (note 69) = 440 Hz
	return 440.0 * math.Pow(2.0, (float64(note)-69.0)/12.0)
}
