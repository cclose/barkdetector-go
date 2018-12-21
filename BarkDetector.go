package main

import (
        "bytes"
	"encoding/binary"
	"bufio"
	"flag"
	"fmt"
	"github.com/gordonklaus/portaudio"
	"github.com/zenwerk/go-wave"
	"log"
	"math"
	"os"
	"os/signal"
	"time"
)


var active bool = true
var writeWave, writeCSV bool
var verbose bool = false
var values map[int64]*TimeValue
var measurementDuration time.Duration

const SIGNED_16BIT_INT = 32767

type SamplePacket struct {
	buffer []float32
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

func (sp *SignalProcessor) handleInput(in []float32) {
	currTime := time.Now()

	sp.processQ <- SamplePacket{in, sp.lastTime, currTime}
	//vlog(fmt.Sprintf("Measured %s of audio\n", currTime.Sub(sp.lastTime)))
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
	currChunk := time.Now().Truncate(measurementDuration).Add(measurementDuration)
	chkId := currChunk.UnixNano()
	var sum float32 = 0
	var samples float32 = 0
	waveBuffer := new(bytes.Buffer)
	if writeCSV {
		csvWriter.WriteString("\n\n\nTimeStamp(NS),dB,RMS,Average,NumberOfSamples\n")
	}
	for {
		select {
		case sample = <-inputQueue:
			//calculate the duration of the sample packet: stop - start
			sampleDur := sample.stop.Sub(sample.start)
			// calculate the duration of each sample by dividing duration by samples
			// this is how long each sample takes
			chunkDur := time.Duration(int64(sampleDur) / int64(len(sample.buffer)))
			//first sample is at the start
			currTime := sample.start
			// our first measurement point is at the start time plus the duration of our measurements
			// trucate is used to keep our samples nice and even and avoid weird edge case logic
			currChunk := currTime.Truncate(measurementDuration).Add(measurementDuration)
			for _, val := range sample.buffer {
                                // The values are -1 to 1, so we Multiply by 100 to make a nicer range
				// There is no science here. SPL can't really be calculated w/o calibration
				// Using the max value of a signed 16bit int gets close, but 100 makes the range more sensitive
				// and this makes bark detection easier
                                val *= 100
				// advance the current time by the sample's duration
				currTime = currTime.Add(chunkDur)
				// if we're past our measurement goal
				if currTime.After(currChunk) {
					// calculate the next goal
					currChunk = currTime.Truncate(measurementDuration).Add(measurementDuration)
					// get the timestamp of this chunk
					chkId = currChunk.UnixNano()
					// calculate the measurement for this chunk
					if samples > 0 {
						//our faux dB calculation. We find the average value for the duration
						avg := float64(sum / samples)
						// Then we take the sqrt to find the root mean squared
						rms := math.Sqrt(avg)
						// I read this on wikipedia for the dB scale being logorithmic
						db := 20 * math.Log10(rms)
						// Print our curent measured level. the \r lets us overwrite the line!!
						fmt.Printf("\r SPL: %f", db)
						if writeCSV {
							// if we're writing to a CSV, we'll do that now
							csvWriter.WriteString(fmt.Sprintf("%d,%f,%f,%f,%f,%.0f\n", chkId, db, rms, avg, sum, samples))
						}
					}
					// reset our counts
					sum = 0
					samples = 0
				}
				// for RMS we need the average value, so add em up and we'll divide by the count when we're done
				sum += val * val
				samples++
			}

			if writeWave {
                                // write our samples into a byte array
				for i:= 0; i < len(sample.buffer); i++ {
					// The wav wants ints, so we convert our floats to ints
					// The floats are -1 to 1, so we multiply by the max value of a 16bit int (because our wav is 16bit)
					// to get the equivalent int value
					// This is then written into a byte buffer in little endian form, because this is what the wave writer
					// prefers. IT can write 1 int at a time, but this is inefficent
					err := binary.Write(waveBuffer, binary.LittleEndian, int16(sample.buffer[i]*SIGNED_16BIT_INT))
					if err != nil {
						log.Fatal(err)
					}
				}
				// write out our wave sample
				_, err := waveWriter.Write(waveBuffer.Bytes())
				if err != nil {
					log.Fatal(err)
				}
				// clear the wave buffer
				waveBuffer.Reset()
			}

		default:
		}
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

// writes log message if verbose is enabled
func vlog(message string) {
	if verbose {
		fmt.Println(message)
	}
}

func main() {
	defer func() {
		//catch exceptions
		if r := recover(); r != nil {
			//handle shutdown here
		}
	}()

	var audioFileName, csvFileName, preset string
	var bufferSize, sampleRate, valPerSecond, runTime int
        var bufferLen float32
	if true {
		writeWaveTmp := flag.Bool("WriteWave", false, "Write a .wav file of the processed input")
		waveFileTmp := flag.String("WaveFile", "barkOut.wav", "The file to write to")
		writeCSVTmp := flag.Bool("WriteCSV", false, "Write a .wav file of the processed input")
		csvFileTmp := flag.String("CSVFile", "barkOut.csv", "The file to write to")
		presetTmp := flag.String("Preset", "hifi", "preset recording values (hifi,midfi,lowfi)")
		bufferSizeTmp := flag.Int("BufferSize", 196608, "Size of framebuffer in bytes")
		bufferLenTmp := flag.Float64("BufferLength", 1, "How many seconds of audio should our buffer hold")
		sampleRateTmp := flag.Int("SampleRate", 44100, "Sample Rate")
		valPerSecondTmp := flag.Int("MeasurementRate", 20, "Numer of measurements per second")
		runTimeTmp := flag.Int("RunTime", 10, "Run for this many seconds")
		verboseTmp := flag.Bool("Verbose", false, "Verbose output mode")
		flag.Parse()

		writeWave = *writeWaveTmp
		audioFileName = *waveFileTmp
		writeCSV = *writeCSVTmp
		csvFileName = *csvFileTmp
		preset = *presetTmp
		bufferLen = float32(*bufferLenTmp)
		bufferSize = *bufferSizeTmp
		sampleRate = *sampleRateTmp
		valPerSecond = *valPerSecondTmp
		verbose = *verboseTmp
                runTime = *runTimeTmp
		if preset != "" {
                        fmt.Println("Using Preset");
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
		measurementDuration = time.Duration(int64(time.Second) / int64(valPerSecond))
	}

	//defaults
	inputChannels := 1
	outputChannels := 0

        //If we specify a buffer duration, calculate size
        if bufferLen != 0 {
		vlog(fmt.Sprintf("  Calculating Buffer Size by duration %f", bufferLen))
		bufferSize = int(float32(sampleRate) * bufferLen)
        }

	vlog(fmt.Sprintf("\n\tSample Rate: %d\n\tBuffer Size: %d", sampleRate, bufferSize))

	// init PortAudio
	portaudio.Initialize()
	defer portaudio.Terminate()

	processQ := make(chan SamplePacket, 1028)
        sigChan := make(chan os.Signal, 1)

        // if SIGINT is received, send signal into our signal channel
        signal.Notify(sigChan, os.Interrupt)
	go func() {
		for active {
			select {
			// we have received an OS signal... quit. We don't actually care about the signal so _ it
			case _ = <-sigChan:
				fmt.Println("Signal received, shutting down!")
				active = false

			// otherwise sleep for a second, then try again
			default:
				time.Sleep(time.Second * 1)
			}
		}

	}()

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
			BitsPerSample: 16, // if 16, change to WriteSample16()
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

	go processInputRT(processQ, done, waveWriter, csvWriter)
	handler := &SignalProcessor{time.Now(), processQ}
	stream, stErr = portaudio.OpenDefaultStream(inputChannels, outputChannels, float64(sampleRate), bufferSize, handler.handleInput)
	handler.lastTime = time.Now()

	if stErr != nil {
		log.Fatal(stErr)
	}
	defer stream.Close()

	start := time.Now()
	if err := stream.Start(); err != nil {
		log.Fatal(err)
	}

	for active {
		time.Sleep(time.Second * 1)
		if time.Since(start) > time.Second * time.Duration(runTime) {
			active = false;
			fmt.Println("Time expired, shutting down!")
		}
	}

	doneTime := time.Now()
	active = false

	fmt.Println("Listener shutdown, waiting for processor")
	<-done
	vlog(fmt.Sprintf("-- Waited %s for processor", time.Since(doneTime)))
}
