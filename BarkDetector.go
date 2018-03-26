package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/gordonklaus/portaudio"
	"github.com/zenwerk/go-wave"
	"log"
	"math"
	"os"
	"time"
)

var active bool = true
var writeWave, writeCSV bool
var verbose bool = false
var realTime bool = false
var values map[int64]*TimeValue
var valueLen time.Duration

type SamplePacket struct {
	buffer []byte
	start  time.Time
	stop   time.Time
}

type TimeValue struct {
	stamp  time.Time
	values []int
}

type SignalProcessor struct {
	lastTime time.Time
	processQ chan SamplePacket
}

func (sp *SignalProcessor) handleInput(out [][]byte) {
	currTime := time.Now()

	sp.processQ <- SamplePacket{out[0], sp.lastTime, currTime}
	if verbose {
		fmt.Printf("Measured %s of audio\n", currTime.Sub(sp.lastTime))
	}
	sp.lastTime = currTime
}

func NewTimeValue(stamp time.Time, numValues int64) *TimeValue {
	newTV := TimeValue{stamp: stamp}
	newTV.values = make([]int, numValues)
	return &newTV
}

func processInputRT(inputQueue chan SamplePacket, done chan bool, waveWriter *wave.Writer, csvWriter *bufio.Writer) {
	var sample SamplePacket
	values = make(map[int64]*TimeValue)
	count := 0
	currChunk := time.Now().Truncate(valueLen).Add(valueLen)
	chkId := currChunk.UnixNano()
	sum := 0
	samples := 0
	if writeCSV {
		csvWriter.WriteString("\n\n\nTimeStamp(NS),dB,RMS,Average,NumberOfSamples\n")
	}
	for {
		select {
		case sample = <-inputQueue:
			sampleDur := sample.stop.Sub(sample.start)
			chunkDur := time.Duration(int64(sampleDur) / int64(len(sample.buffer)))
			currTime := sample.start
			//currSampleChunk := sample.start.Truncate(valueLen).Add(valueLen)
			for _, val := range sample.buffer {
				intVal := int(val) / 127
				currTime = currTime.Add(chunkDur)
				if currTime.After(currChunk) {
					currChunk = currTime.Truncate(valueLen).Add(valueLen)
					chkId = currChunk.UnixNano()
					if samples > 0 {
						avg := float64(sum / samples)
						rms := math.Sqrt(avg)
						db := 20 * math.Log10(rms)
						//db := 20 * math.Log10(math.Sqrt(float64(sum/samples)))
						fmt.Printf("\r SPL: %f", db)
						if writeCSV {
							csvWriter.WriteString(fmt.Sprintf("%d,%f,%f,%f,%d,%d\n", chkId, db, rms, avg, sum, samples))
						}
					}
					sum = 0
					samples = 0
				}
				sum += intVal * intVal
				samples++
			}

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
				if writeCSV {
					csvWriter.Flush()
				}
				done <- true
				return
			}
			//time.Sleep(time.Millisecond * 25) //else sleep for a bit and try again
		}
	}

}

func processInput(inputQueue chan SamplePacket, done chan bool, waveWriter *wave.Writer, csvWriter *bufio.Writer) {
	var sample SamplePacket
	values = make(map[int64]*TimeValue)
	count := 0
	for {
		select {
		case sample = <-inputQueue:
			if verbose {
				fmt.Println("--Processor got input")
			}
			sampleDur := sample.stop.Sub(sample.start)
			chunkDur := time.Duration(int64(sampleDur) / int64(len(sample.buffer)))
			smpsPerChunk := int64(int64(len(sample.buffer)) / int64(sampleDur))
			smpsPerChunk++ //round up

			currTime := sample.start
			currChunk := sample.start.Truncate(valueLen).Add(valueLen)
			chkId := currChunk.UnixNano()
			for _, val := range sample.buffer {
				currTime = currTime.Add(chunkDur)
				if currTime.After(currChunk) {
					currChunk = currChunk.Add(valueLen)
					chkId = currChunk.UnixNano()
				}
				tv, ok := values[chkId]
				if !ok {
					tv = NewTimeValue(currChunk, smpsPerChunk)
					values[chkId] = tv
				}
				tv.values = append(tv.values, int(val))
			}
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
				if writeCSV {
					writeValues(csvWriter)
				}
				done <- true
				return
			}
			time.Sleep(time.Millisecond * 500) //else sleep for a bit and try again
		}

	}

}

func writeValues(csvWriter *bufio.Writer) {

	for key, tv := range values {
		var sum = 0
		numValues := len(tv.values)
		for i := 0; i < numValues; i++ {
			sum += tv.values[i] * tv.values[i]
		}
		avg := 20 * math.Log10(math.Sqrt(float64(sum/len(values)))/127)
		csvWriter.WriteString(fmt.Sprintf("%d,%f\n", key, avg))
	}

	csvWriter.Flush()
}

func main() {
	defer func() {
		//catch exceptions
		if r := recover(); r != nil {
			//handle shutdown here
		}
	}()

	var audioFileName, csvFileName, preset string
	var bufferSize, sampleRate, valPerSecond int
	if true {
		writeWaveTmp := flag.Bool("WriteWave", false, "Write a .wav file of the processed input")
		waveFileTmp := flag.String("WaveFile", "barkOut.wav", "The file to write to")
		writeCSVTmp := flag.Bool("WriteCSV", false, "Write a .wav file of the processed input")
		csvFileTmp := flag.String("CSVFile", "barkOut.csv", "The file to write to")
		presetTmp := flag.String("Preset", "hifi", "preset recording values (hifi,midfi,lowfi)")
		bufferSizeTmp := flag.Int("BufferSize", 196608, "Size of framebuffer in bytes")
		sampleRateTmp := flag.Int("SampleRate", 44100, "Sample Rate")
		valPerSecondTmp := flag.Int("MeasurementRate", 20, "Numer of measurements per second")
		realTimeTmp := flag.Bool("RealTime", false, "RealTime mode")
		verboseTmp := flag.Bool("Verbose", false, "Verbose output mode")
		flag.Parse()

		writeWave = *writeWaveTmp
		audioFileName = *waveFileTmp
		writeCSV = *writeCSVTmp
		csvFileName = *csvFileTmp
		preset = *presetTmp
		bufferSize = *bufferSizeTmp
		sampleRate = *sampleRateTmp
		valPerSecond = *valPerSecondTmp
		verbose = *verboseTmp
		realTime = *realTimeTmp
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
		valueLen = time.Duration(int64(time.Second) / int64(valPerSecond))
	}

	//defaults
	inputChannels := 1
	outputChannels := 0
	framesPerBuffer := make([]byte, bufferSize)

	// init PortAudio
	portaudio.Initialize()
	defer portaudio.Terminate()

	processQ := make(chan SamplePacket, 1028)

	var waveWriter *wave.Writer
	var csvWriter *bufio.Writer
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
	if writeCSV {
		csvFile, err := os.Create(csvFileName)
		if err != nil {
			log.Fatal(err)
		}
		defer csvFile.Close()
		csvWriter = bufio.NewWriter(csvFile)
	}

	done := make(chan bool, 1)

	var stErr error
	var stream *portaudio.Stream
	if realTime {
		go processInputRT(processQ, done, waveWriter, csvWriter)
		handler := &SignalProcessor{time.Now(), processQ}
		stream, stErr = portaudio.OpenDefaultStream(inputChannels, outputChannels, float64(sampleRate), 1024, handler.handleInput)
		handler.lastTime = time.Now()
	} else {
		go processInput(processQ, done, waveWriter, csvWriter)
		stream, stErr = portaudio.OpenDefaultStream(inputChannels, outputChannels, float64(sampleRate), len(framesPerBuffer), framesPerBuffer)
	}
	if stErr != nil {
		log.Fatal(stErr)
	}
	defer stream.Close()

	start := time.Now()
	if err := stream.Start(); err != nil {
		log.Fatal(err)
	}

	if realTime {
		time.Sleep(time.Second * 10)

	} else {

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
	}
	doneTime := time.Now()
	active = false

	fmt.Println("Listener shutdown, waiting for processor")
	<-done
	if verbose {
		fmt.Printf("-- Waited %s for processor\n", time.Since(doneTime))
	}
}
