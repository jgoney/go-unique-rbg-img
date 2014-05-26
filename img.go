package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"image/png"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"
)

var imageDir string
var colorDepth, dim, distance, depth int
var procs int = runtime.GOMAXPROCS(runtime.NumCPU())

func init() {
	wd, e := os.Getwd()
	check(e)
	flag.StringVar(&imageDir, "f", wd, "The directory where the image output will be stored.")
	flag.IntVar(&distance, "d", 125, "The color distance threshold to be used. Larger values execute quickly, but with noisier results.")
	flag.IntVar(&depth, "c", 6, "The color bit depth per channel to be used.")
}

// An RGBA color type with atomic operation support.
type ColorAtomic struct {
	R, G, B, A uint8
	Checked bool
	mutex sync.Mutex
}

func (ca ColorAtomic) RGBA() (r, g, b, a uint32) {
    r = uint32(ca.R)
    r |= r << 8
    g = uint32(ca.G)
    g |= g << 8
    b = uint32(ca.B)
    b |= b << 8
    a = uint32(ca.A)
    a |= a << 8
    return
}

type ColorArray [] *ColorAtomic

// Functions to enable sorting on ColorArray
func (a ColorArray) Len() int {
	return len(a)
}

func (a ColorArray) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a ColorArray) Less(i, j int) bool {
	x := (a[i].R + a[i].G + a[i].B)
	y := (a[j].R + a[j].G + a[j].B)
	return x < y
}

// Generic error check function.
func check(e error) {
    if e != nil {
        panic(e)
    }
}

func WriteImageFile(fileName string, m image.Image) {
	fmt.Printf("Trying to open file %s...", fileName)

	f, e := os.Create(fileName)
	defer f.Close()
	check(e)

	if e == nil {
		fmt.Println("success!")
	}

	w := bufio.NewWriter(f)

	png.Encode(w, m)
	w.Flush()
}

func PopImage(colors ColorArray) image.Image {

	m := image.NewRGBA(image.Rect(0, 0, dim, dim))
	b := m.Bounds()
	
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := x + y * (b.Max.Y - b.Min.Y)
			m.Set(x, y, colors[p])
		}
	}

	return m
}

// A simple tone mapper. Scales the RGB values to use the full range of 
func ToneMapper(c *ColorAtomic) {
	m := uint8(256 / colorDepth)
	c.R = c.R * m
	c.G = c.G * m
	c.B = c.B * m
}

func GenLinearColors(do_map bool) ColorArray {
	// Calculate all possible RGB colors by marching straight through the possible values in a nested loop
	colors := make(ColorArray, dim * dim)
	i := 0
	for r := 0; r < colorDepth; r++ {
		for g := 0; g < colorDepth; g++ {
			for b := 0; b < colorDepth; b++ {
				c := new(ColorAtomic)
				c.R, c.G, c.B, c.A = uint8(r), uint8(g), uint8(b), uint8(255)
				if do_map {
					ToneMapper(c)
				}
				colors[i] = c
				i++
			}
		}
	}

	return colors
}

func GenShuffledColors(reseed, do_map bool) ColorArray {
	// Calculate all possible RGB colors via a nested loops, where the possible values have been shuffled
	if reseed {
		rand.Seed(time.Now().UnixNano())
	}

	colors := make(ColorArray, dim * dim)
	i := 0
	for _, r := range rand.Perm(colorDepth) {
		for _, g := range rand.Perm(colorDepth) {
			for _, b := range rand.Perm(colorDepth) {
				c := new(ColorAtomic)
				c.R, c.G, c.B, c.A = uint8(r), uint8(g), uint8(b), uint8(255)
				if do_map {
					ToneMapper(c)
				}
				colors[i] = c
				i++
			}
		}
	}

	return colors
}

func GenRandomColors() ColorArray {
	colors := GenShuffledColors(true, true)

	for i := range colors {
    	j := rand.Intn(i + 1)
    	colors[i], colors[j] = colors[j], colors[i]
	}

	return colors
}

func GenSortColors() ColorArray {
	colors := GenLinearColors(true)

	sort.Stable(ColorArray(colors))

	return colors
}

func GenDistSortColors(dist float64, colors ColorArray) chan ColorArray {

	out_chan := make(chan ColorArray)
	go func() {
		for _, v := range colors {
			v.mutex.Lock()
			if !v.Checked {
				r_color := v
				v.Checked = true

				// Make new slice for "distance colors", reinit (in loop), append random color
				d_colors := make(ColorArray, dim * dim)
				d_colors = nil
				d_colors = append(d_colors, r_color)

				// Traverse colors array looking for colors within the threshold
				for _, w := range colors {

					if !w.Checked && color_distance(r_color, w) < dist {
						w.Checked = true
						d_colors = append(d_colors, w)
					}
				}

				out_chan <- d_colors
			}
			v.mutex.Unlock()
		}
		close(out_chan)
	}()

	return out_chan
}

// Calculates the distance between two points in a 3D space.
func color_distance(i, j *ColorAtomic) float64 {
	return math.Sqrt(math.Pow(float64(j.R - i.R), 2) + math.Pow(float64(j.G - i.G), 2) + math.Pow(float64(j.B - i.B), 2))
}

// Stolen directly from http://blog.golang.org/pipelines
func merge(cs ...chan ColorArray) chan ColorArray {
    var wg sync.WaitGroup
    out := make(chan ColorArray)

    // Start an output goroutine for each input channel in cs.  output
    // copies values from c to out until c is closed, then calls wg.Done.
    output := func(c <-chan ColorArray) {
        for n := range c {
            out <- n
        }
        wg.Done()
    }
    wg.Add(len(cs))
    for _, c := range cs {
        go output(c)
    }

    // Start a goroutine to close out once all the output goroutines are
    // done.  This must start after the wg.Add call.
    go func() {
        wg.Wait()
        close(out)
    }()
    return out
}

// Print a simple menu.
func printMenu() {
	fmt.Println("Please choose from the following options (press 'q' to quit):")
	fmt.Println("1. Linear colors")
	fmt.Println("2. Shuffled colors (random by channel, creates bands)")
	fmt.Println("3. Random colors (creates noise)")		
	fmt.Println("4. Simple sorted colors")
	fmt.Println("5. Distance sorted colors")
	fmt.Println("")
}

func printStat() {
	fmt.Printf("Using %d cores\n", procs)
	fmt.Printf("Writing image to %s\n", imageDir)
}

func main() {

	flag.Parse()

	colorDepth = int(math.Pow(2, float64(depth)))

	// Cube our color depth, then take the square root to calculate the image dimensions
	dim = int(math.Sqrt(math.Pow(float64(colorDepth), 3)))

	done := false
	var choice, funcName string
	var colors ColorArray

	printStat()	

	for done == false {
		printMenu()
		fmt.Scanln(&choice)
		switch choice {
			case "1":
				fmt.Printf("Generating %dx%d linear image...\n", dim, dim)
				funcName = "linear"
				colors = GenLinearColors(true)
			case "2":
				fmt.Printf("Generating %dx%d shuffled image...\n", dim, dim)
				funcName = "shuffled"
				colors = GenShuffledColors(true, true)
			case "3":
				fmt.Printf("Generating %dx%d randomized image...\n", dim, dim)
				funcName = "random"
				colors = GenRandomColors()
			case "4":
				fmt.Printf("Generating %dx%d sorted image (stable sort, additive method)...\n", dim, dim)
				funcName = "sorted"
				colors = GenSortColors()
			case "5":
				fmt.Printf("Generating %dx%d distance sorted image (this may take a while)...\n", dim, dim)
				d := float64(distance)
				funcName = fmt.Sprintf("distance_%d", int(d))
				inColors := GenRandomColors()

				chans := make([]chan ColorArray, procs*2)
				for i := 0; i < len(chans); i++ {
					chans[i] = GenDistSortColors(d, inColors)
				}

				colors = make(ColorArray, dim * dim)
				colors = nil  // I have no idea why this is necessary

			    for n := range merge(chans...) {
					colors = append(colors, n...)
					fmt.Printf("Processed colors: %d of %d (%3.2f%%)\r", len(colors), (dim * dim), (float64(len(colors)) / math.Pow(float64(dim), 2) * 100.0))
				}
				fmt.Println("")

				// This is somewhat naïve error checking, since it just prints a warning. It's good enough for this purpose, though.
				if len(colors) != int(math.Pow(float64(colorDepth), 3)) {
					fmt.Printf("ERROR ERROR ERROR --> %d != %d\n", len(colors), int(math.Pow(float64(colorDepth), 3)))
				}
			case "q":
				done = true
				fmt.Println("Exiting.")
				os.Exit(0)
			default:
				fmt.Printf("'%s' is not a valid choice, please try again.\n", choice)
		}
		if funcName != "" && colors != nil {
			fileName := fmt.Sprintf("%s/%s_%d.png", imageDir, funcName, colorDepth)
			WriteImageFile(fileName, PopImage(colors))
		}
	}
}
