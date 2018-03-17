package main

import (
	"flag"
	"fmt"
	"github.com/gordonklaus/portaudio"
	"github.com/zenwerk/go-wave"
	"log"
	"os"
	"time"
)

var active bool = true
var writeWave bool = true
var verbose bool = false

type SamplePacket struct {
	buffer []byte
	start  time.Time
	stop   time.Time
}

func processInput(inputQueue chan SamplePacket, done chan bool, waveWriter *wave.Writer) {
	var sample SamplePacket
	printedSlice := 2
	count := 0
	for {
		select {
		case sample = <-inputQueue:
			if verbose {
				fmt.Println("--Processor got input")
			}
			//TODO DELETE
			if printedSlice > 0 {
				//for i, val := range sample.buffer {
				//	fmt.Printf("%d-%x|", i, val)
				//}
				//fmt.Println()
				printedSlice--
			}
			//TODO DELETE END
			count++

			if writeWave {
				_, err := waveWriter.Write([]byte(sample.buffer)) // WriteSample16 for 16 bits
				if err != nil {
					log.Fatal(err)
				}
			}

		default:
			//if we're not active, signal the mothership and return
			if !active {
				fmt.Println("-Processor read kill signal")
				fmt.Printf("Processed %d packets\n", count)
				if writeWave {
					waveWriter.Close()
				}
				done <- true
				return
			}
			time.Sleep(time.Millisecond * 500) //else sleep for a bit and try again
		}

	}

}

func main() {
	defer func() {
		//catch exceptions
		if r := recover(); r != nil {
			//handle shutdown here
		}
	}()

	var audioFileName, preset string
	var bufferSize, sampleRate int
	if true {
		writeWaveTmp := flag.Bool("WriteWave", false, "Write a .wav file of the processed input")
		waveFileTmp := flag.String("WaveFile", "barkOut.wav", "The file to write to")
		presetTmp := flag.String("Preset", "hifi", "preset recording values (hifi,midfi,lowfi)")
		bufferSizeTmp := flag.Int("BufferSize", 196608, "Size of framebuffer in bytes")
		sampleRateTmp := flag.Int("SampleRate", 44100, "Sample Rate")
		verboseTmp := flag.Bool("Verbose", false, "Verbose output mode")
		flag.Parse()

		writeWave = *writeWaveTmp
		audioFileName = *waveFileTmp
		preset = *presetTmp
		bufferSize = *bufferSizeTmp
		sampleRate = *sampleRateTmp
		verbose = *verboseTmp
		if preset != "" {
			switch preset {
			case "hifi":
				sampleRate = 44100
				bufferSize = 196608
				break
			case "midfi":
				sampleRate = 22050
				bufferSize = 98304
				break
			case "lofi":
				sampleRate = 11025
				bufferSize = 49152
				break
			default:
				log.Fatal("invalid preset " + preset)
			}
		}
	}

	//defaults
	inputChannels := 1
	outputChannels := 0
	framesPerBuffer := make([]byte, bufferSize)

	// init PortAudio
	portaudio.Initialize()
	defer portaudio.Terminate()

	stream, err := portaudio.OpenDefaultStream(inputChannels, outputChannels, float64(sampleRate), len(framesPerBuffer), framesPerBuffer)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	var waveWriter *wave.Writer
	if writeWave {
		waveFile, err := os.Create(audioFileName)
		if err != nil {
			log.Fatal(err)
		}
		param := wave.WriterParam{
			Out:           waveFile,
			Channel:       inputChannels,
			SampleRate:    sampleRate,
			BitsPerSample: 8, // if 16, change to WriteSample16()
		}

		waveWriter, err = wave.NewWriter(param)
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := stream.Start(); err != nil {
		log.Fatal(err)
	}

	done := make(chan bool, 1)
	processQ := make(chan SamplePacket, 1028)
	start := time.Now()

	go processInput(processQ, done, waveWriter)

	hasFailed := 0
	FAIL_LIMIT := 5
	for {
		stopwatch := time.Now()
		if err := stream.Read(); err != nil {
			fmt.Println("Error reading stream: " + err.Error() + "\n")
			if hasFailed >= FAIL_LIMIT {
				os.Exit(1)
			} //implicit else
			hasFailed++
			time.Sleep(time.Millisecond * 1)
			continue
		}

		stopTime := time.Now()
		if verbose {
			fmt.Printf("Measured %s of audio\n", stopTime.Sub(stopwatch))
		}

		processQ <- SamplePacket{[]byte(framesPerBuffer), stopwatch, stopTime}

		//reset fail counter
		if hasFailed > 0 {
			hasFailed--
		}

		if time.Since(start) > time.Second*10 {
			break
		}

	}
	doneTime := time.Now()
	active = false

	fmt.Println("Listener shutdown, waiting for processor")
	<-done
	if verbose {
		fmt.Printf("-- Waited %s for processor\n", time.Since(doneTime))
	}
}
