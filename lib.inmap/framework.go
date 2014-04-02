package inmap

import (
	"bitbucket.org/ctessum/aqhealth"
	"encoding/gob"
	"fmt"
	"github.com/twpayne/gogeom/geom"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
)

type InMAPdata struct {
	Data          []*Cell // One data holder for each grid cell
	Dt            float64 // seconds
	Nlayers       int     // number of model layers
	LayerStart    []int   // start index of each layer (inclusive)
	LayerEnd      []int   // end index of each layer (exclusive)
	westBoundary  []*Cell // boundary cells
	eastBoundary  []*Cell // boundary cells
	northBoundary []*Cell // boundary cells
	southBoundary []*Cell // boundary cells
	topBoundary   []*Cell // boundary cells; assume bottom boundary is the same as lowest layer
}

func init() {
	gob.Register(geom.Polygon{})
}

// Data for a single grid cell
type Cell struct {
	Geom                           geom.T       // Cell geometry
	WebMapGeom                     geom.T       // Cell geometry in web map (mercator) coordinate system
	UPlusSpeed, UMinusSpeed        float64      // [m/s]
	VPlusSpeed, VMinusSpeed        float64      // [m/s]
	WPlusSpeed, WMinusSpeed        float64      // [m/s]
	OrgPartitioning, SPartitioning float64      // gaseous fraction
	NOPartitioning, NHPartitioning float64      // gaseous fraction
	ParticleWetDep, SO2WetDep      float64      // wet deposition rate [1/s]
	OtherGasWetDep                 float64      // wet deposition rate [1/s]
	ParticleDryDep, NH3DryDep      float64      // Dry deposition velocities [m/s]
	SO2DryDep, VOCDryDep           float64      // Dry deposition velocities [m/s]
	NOxDryDep                      float64      // Dry deposition velocities [m/s]
	SO2oxidation                   float64      // SO2 oxidation to SO4 by HO and H2O2 [1/s]
	Kzz                            float64      // Grid center vertical diffusivity after applying convective fraction [m2/s]
	KzzAbove, KzzBelow             []float64    // horizontal diffusivity [m2/s] (staggered grid)
	Kxxyy                          float64      // Grid center horizontal diffusivity [m2/s]
	KyySouth, KyyNorth             []float64    // horizontal diffusivity [m2/s] (staggered grid)
	KxxWest, KxxEast               []float64    // horizontal diffusivity at [m2/s] (staggered grid)
	M2u                            float64      // ACM2 upward mixing (Pleim 2007) [1/s]
	M2d                            float64      // ACM2 downward mixing (Pleim 2007) [1/s]
	TotalPop, WhitePop             float64      // Population [people/grid cell]
	TotalPoor, WhitePoor           float64      // Poor population [people/grid cell]
	AllCauseMortality              float64      // Mortalities per 100,000 people per year
	PblTopLayer                    float64      // k index of boundary layer top
	Dx, Dy, Dz                     float64      // grid size [meters]
	Volume                         float64      // [cubic meters]
	Row                            int          // master cell index
	Ci                             []float64    // concentrations at beginning of time step [μg/m3]
	Cf                             []float64    // concentrations at end of time step [μg/m3]
	emisFlux                       []float64    //  emissions [μg/m3/s]
	West                           []*Cell      // Neighbors to the East
	East                           []*Cell      // Neighbors to the West
	South                          []*Cell      // Neighbors to the South
	North                          []*Cell      // Neighbors to the North
	Below                          []*Cell      // Neighbors below
	Above                          []*Cell      // Neighbors above
	GroundLevel                    []*Cell      // Neighbors at ground level
	WestFrac, EastFrac             []float64    // Fraction of cell covered by each neighbor (adds up to 1).
	NorthFrac, SouthFrac           []float64    // Fraction of cell covered by each neighbor (adds up to 1).
	AboveFrac, BelowFrac           []float64    // Fraction of cell covered by each neighbor (adds up to 1).
	GroundLevelFrac                []float64    // Fraction of cell above to each ground level cell (adds up to 1).
	IWest                          []int        // Row indexes of neighbors to the East
	IEast                          []int        // Row indexes of neighbors to the West
	ISouth                         []int        // Row indexes of neighbors to the South
	INorth                         []int        // Row indexes of neighbors to the north
	IBelow                         []int        // Row indexes of neighbors below
	IAbove                         []int        // Row indexes of neighbors above
	IGroundLevel                   []int        // Row indexes of neighbors at ground level
	DxPlusHalf                     []float64    // Distance between centers of cell and East [m]
	DxMinusHalf                    []float64    // Distance between centers of cell and West [m]
	DyPlusHalf                     []float64    // Distance between centers of cell and North [m]
	DyMinusHalf                    []float64    // Distance between centers of cell and South [m]
	DzPlusHalf                     []float64    // Distance between centers of cell and Above [m]
	DzMinusHalf                    []float64    // Distance between centers of cell and Below [m]
	Layer                          int          // layer index of grid cell
	Temperature                    float64      // Average temperature, K
	WindSpeed                      float64      // RMS wind speed, [m/s]
	S1                             float64      // stability parameter [?]
	SClass                         float64      // stability class: "0=Unstable; 1=Stable
	lock                           sync.RWMutex // Avoid cell being written by one subroutine and read by another at the same time.
}

func (c *Cell) prepare() {
	c.Volume = c.Dx * c.Dy * c.Dz
	c.Ci = make([]float64, len(polNames))
	c.Cf = make([]float64, len(polNames))
	c.emisFlux = make([]float64, len(polNames))
}

func (c *Cell) makecopy() *Cell {
	c2 := new(Cell)
	c2.Dx, c2.Dy, c2.Dz = c.Dx, c.Dy, c.Dz
	c2.Kxxyy = c.Kxxyy
	c2.prepare()
	return c2
}

// Initialize the model, where `filename` is the path to
// the Gob files with meteorology and background concentration data
// (where `[layer]` is a stand-in for the layer number),
// `nLayers` is the number of vertical layers in the model,
// and `httpPort` is the port number for hosting the html GUI.
func InitInMAPdata(filetemplate string, nLayers int, httpPort string) *InMAPdata {
	inputData := make([][]*Cell, nLayers)
	d := new(InMAPdata)
	d.Nlayers = nLayers
	d.LayerStart = make([]int, nLayers)
	d.LayerEnd = make([]int, nLayers)
	var wg sync.WaitGroup
	wg.Add(nLayers)
	for k := 0; k < nLayers; k++ {
		go func(k int) {
			filename := strings.Replace(filetemplate, "[layer]",
				fmt.Sprintf("%v", k), -1)
			f, err := os.Open(filename)
			if err != nil {
				fmt.Println(err.Error())
				os.Exit(1)
			}
			g := gob.NewDecoder(f)
			g.Decode(&inputData[k])
			d.LayerStart[k] = 0
			d.LayerEnd[k] = len(inputData[k])
			f.Close()
			wg.Done()
		}(k)
	}
	wg.Wait()
	ncells := 0
	// Adjust so beginning of layer is at end of previous layer
	for k := 0; k < nLayers; k++ {
		d.LayerStart[k] += ncells
		d.LayerEnd[k] += ncells
		ncells += len(inputData[k])
	}
	// set up data holders
	d.Data = make([]*Cell, ncells)
	for _, indata := range inputData {
		for _, c := range indata {
			c.prepare()
			d.Data[c.Row] = c
		}
	}
	d.westBoundary = make([]*Cell, 0, 200)
	d.eastBoundary = make([]*Cell, 0, 200)
	d.southBoundary = make([]*Cell, 0, 200)
	d.northBoundary = make([]*Cell, 0, 200)
	d.topBoundary = make([]*Cell, 0, 200)
	nprocs := runtime.GOMAXPROCS(0)
	wg.Add(nprocs)
	for procNum := 0; procNum < nprocs; procNum++ {
		go func(procNum int) {
			for ii := procNum; ii < len(d.Data); ii += nprocs {
				cell := d.Data[ii]
				// Link cells to neighbors and/or boundaries.
				if len(cell.IWest) == 0 {
					c := cell.makecopy()
					cell.West = []*Cell{c}
					d.westBoundary = append(d.westBoundary, c)
				} else {
					cell.West = make([]*Cell, len(cell.IWest))
					for i, row := range cell.IWest {
						cell.West[i] = d.Data[row]
					}
					cell.IWest = nil
				}
				if len(cell.IEast) == 0 {
					c := cell.makecopy()
					cell.East = []*Cell{c}
					d.eastBoundary = append(d.eastBoundary, c)
				} else {
					cell.East = make([]*Cell, len(cell.IEast))
					for i, row := range cell.IEast {
						cell.East[i] = d.Data[row]
					}
					cell.IEast = nil
				}
				if len(cell.ISouth) == 0 {
					c := cell.makecopy()
					cell.South = []*Cell{c}
					d.southBoundary = append(d.southBoundary, c)
				} else {
					cell.South = make([]*Cell, len(cell.ISouth))
					for i, row := range cell.ISouth {
						cell.South[i] = d.Data[row]
					}
					cell.ISouth = nil
				}
				if len(cell.INorth) == 0 {
					c := cell.makecopy()
					cell.North = []*Cell{c}
					d.northBoundary = append(d.northBoundary, c)
				} else {
					cell.North = make([]*Cell, len(cell.INorth))
					for i, row := range cell.INorth {
						cell.North[i] = d.Data[row]
					}
					cell.INorth = nil
				}
				if len(cell.IAbove) == 0 {
					c := cell.makecopy()
					cell.Above = []*Cell{c}
					d.topBoundary = append(d.topBoundary, c)
				} else {
					cell.Above = make([]*Cell, len(cell.IAbove))
					for i, row := range cell.IAbove {
						cell.Above[i] = d.Data[row]
					}
					cell.IAbove = nil
				}
				if cell.Layer != 0 {
					cell.Below = make([]*Cell, len(cell.IBelow))
					cell.GroundLevel = make([]*Cell, len(cell.IGroundLevel))
					for i, row := range cell.IBelow {
						cell.Below[i] = d.Data[row]
					}
					for i, row := range cell.IGroundLevel {
						cell.GroundLevel[i] = d.Data[row]
					}
					cell.IBelow = nil
					cell.IGroundLevel = nil
				} else { // assume bottom boundary is the same as lowest layer.
					cell.Below = []*Cell{d.Data[cell.Row]}
					cell.GroundLevel = []*Cell{d.Data[cell.Row]}
				}
				cell.neighborInfo()
			}
			wg.Done()
		}(procNum)
	}
	wg.Wait()
	d.setTstepCFL() // Set time step
	//d.setTstepRuleOfThumb() // Set time step
	go d.WebServer(httpPort)
	return d
}

// Calculate center-to-center cell distance,
// fractions of grid cell covered by each neighbor
// and harmonic mean staggered-grid diffusivities.
func (cell *Cell) neighborInfo() {
	cell.DxPlusHalf = make([]float64, len(cell.East))
	cell.EastFrac = make([]float64, len(cell.East))
	cell.KxxEast = make([]float64, len(cell.East))
	for i, c := range cell.East {
		cell.DxPlusHalf[i] = (cell.Dx + c.Dx) / 2.
		cell.EastFrac[i] = min(c.Dy/cell.Dy, 1.)
		cell.KxxEast[i] = harmonicMean(cell.Kxxyy, c.Kxxyy)
	}
	cell.DxMinusHalf = make([]float64, len(cell.West))
	cell.WestFrac = make([]float64, len(cell.West))
	cell.KxxWest = make([]float64, len(cell.West))
	for i, c := range cell.West {
		cell.DxMinusHalf[i] = (cell.Dx + c.Dx) / 2.
		cell.WestFrac[i] = min(c.Dy/cell.Dy, 1.)
		cell.KxxWest[i] = harmonicMean(cell.Kxxyy, c.Kxxyy)
	}
	cell.DyPlusHalf = make([]float64, len(cell.North))
	cell.NorthFrac = make([]float64, len(cell.North))
	cell.KyyNorth = make([]float64, len(cell.North))
	for i, c := range cell.North {
		cell.DyPlusHalf[i] = (cell.Dy + c.Dy) / 2.
		cell.NorthFrac[i] = min(c.Dx/cell.Dx, 1.)
		cell.KyyNorth[i] = harmonicMean(cell.Kxxyy, c.Kxxyy)
	}
	cell.DyMinusHalf = make([]float64, len(cell.South))
	cell.SouthFrac = make([]float64, len(cell.South))
	cell.KyySouth = make([]float64, len(cell.South))
	for i, c := range cell.South {
		cell.DyMinusHalf[i] = (cell.Dy + c.Dy) / 2.
		cell.SouthFrac[i] = min(c.Dx/cell.Dx, 1.)
		cell.KyySouth[i] = harmonicMean(cell.Kxxyy, c.Kxxyy)
	}
	cell.DzPlusHalf = make([]float64, len(cell.Above))
	cell.AboveFrac = make([]float64, len(cell.Above))
	cell.KzzAbove = make([]float64, len(cell.Above))
	for i, c := range cell.Above {
		cell.DzPlusHalf[i] = (cell.Dz + c.Dz) / 2.
		cell.AboveFrac[i] = min((c.Dx*c.Dy)/(cell.Dx*cell.Dy), 1.)
		cell.KzzAbove[i] = harmonicMean(cell.Kzz, c.Kzz)
	}
	cell.DzMinusHalf = make([]float64, len(cell.Below))
	cell.BelowFrac = make([]float64, len(cell.Below))
	cell.KzzBelow = make([]float64, len(cell.Below))
	for i, c := range cell.Below {
		cell.DzMinusHalf[i] = (cell.Dz + c.Dz) / 2.
		cell.BelowFrac[i] = min((c.Dx*c.Dy)/(cell.Dx*cell.Dy), 1.)
		cell.KzzBelow[i] = harmonicMean(cell.Kzz, c.Kzz)
	}
	cell.GroundLevelFrac = make([]float64, len(cell.GroundLevel))
	for i, c := range cell.GroundLevel {
		cell.GroundLevelFrac[i] = min((c.Dx*c.Dy)/(cell.Dx*cell.Dy), 1.)
	}
}

// Add in emissions flux to each cell at every time step, also
// set initial concentrations to final concentrations from previous
// time step, and set old velocities to velocities from previous time
// step.
func (c *Cell) addEmissionsFlux(d *InMAPdata) {
	for i, _ := range polNames {
		c.Cf[i] += c.emisFlux[i] * d.Dt
		c.Ci[i] = c.Cf[i]
	}
}

//  Set the time step using the Courant–Friedrichs–Lewy (CFL) condition.
func (d *InMAPdata) setTstepCFL() {
	const Cmax = 1.
	val := 0.
	for _, c := range d.Data {
		thisval := max(c.UPlusSpeed/c.Dx, c.UMinusSpeed/c.Dx,
			c.VPlusSpeed/c.Dy, c.VMinusSpeed/c.Dy,
			c.WPlusSpeed/c.Dz, c.WMinusSpeed/c.Dz)
		if thisval > val {
			val = thisval
		}
	}
	d.Dt = Cmax / math.Pow(3., 0.5) / val // seconds
}

//  Set the time step using the WRF rule of thumb.
func (d *InMAPdata) setTstepRuleOfThumb() {
	d.Dt = d.Data[0].Dx / 1000. * 6
}

func harmonicMean(a, b float64) float64 {
	return 2. * a * b / (a + b)
}

// Convert the concentration data into a regular array
func (d *InMAPdata) toArray(pol string, layer int) []float64 {
	o := make([]float64, d.LayerEnd[layer]-d.LayerStart[layer])
	for i, c := range d.Data[d.LayerStart[layer]:d.LayerEnd[layer]] {
		c.lock.RLock()
		switch pol {
		case "VOC":
			o[i] = c.Cf[igOrg]
		case "SOA":
			o[i] = c.Cf[ipOrg]
		case "PrimaryPM2_5":
			o[i] = c.Cf[iPM2_5]
		case "TotalPM2_5":
			o[i] = c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
		case "Total deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.TotalPop,
				c.AllCauseMortality)
		case "White deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.WhitePop,
				c.AllCauseMortality)
		case "Non-white deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.TotalPop-c.WhitePop,
				c.AllCauseMortality)
		case "High income deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.TotalPop-c.TotalPoor,
				c.AllCauseMortality)
		case "Low income deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.TotalPoor,
				c.AllCauseMortality)
		case "High income white deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.WhitePop-c.WhitePoor,
				c.AllCauseMortality)
		case "Low income non-white deaths":
			pm25 := c.Cf[iPM2_5] + c.Cf[ipOrg] + c.Cf[ipNH] + c.Cf[ipS] + c.Cf[ipNO]
			rr := aqhealth.RRpm25Linear(pm25)
			o[i] = aqhealth.Deaths(rr, c.TotalPoor-c.WhitePoor,
				c.AllCauseMortality)
		case "Population":
			o[i] = c.TotalPop / c.Dx / c.Dy
		case "Baseline mortality rate":
			o[i] = c.AllCauseMortality
		case "NH3":
			o[i] = c.Cf[igNH] / NH3ToN
		case "pNH4":
			o[i] = c.Cf[ipNH] * NtoNH4
		case "SOx":
			o[i] = c.Cf[igS] / SOxToS
		case "pSO4":
			o[i] = c.Cf[ipS] * StoSO4
		case "NOx":
			o[i] = c.Cf[igNO] / NOxToN
		case "pNO3":
			o[i] = c.Cf[ipNO] * NtoNO3
		case "VOCemissions":
			o[i] = c.emisFlux[igOrg]
		case "NOxemissions":
			o[i] = c.emisFlux[igNO]
		case "NH3emissions":
			o[i] = c.emisFlux[igNH]
		case "SOxemissions":
			o[i] = c.emisFlux[igS]
		case "PM2_5emissions":
			o[i] = c.emisFlux[iPM2_5]
		case "UPlusSpeed":
			o[i] = c.UPlusSpeed
		case "UMinusSpeed":
			o[i] = c.UMinusSpeed
		case "VPlusSpeed":
			o[i] = c.VPlusSpeed
		case "VMinusSpeed":
			o[i] = c.VMinusSpeed
		case "WPlusSpeed":
			o[i] = c.WPlusSpeed
		case "WMinusSpeed":
			o[i] = c.WMinusSpeed
		case "Organicpartitioning":
			o[i] = c.OrgPartitioning
		case "Sulfurpartitioning":
			o[i] = c.SPartitioning
		case "Nitratepartitioning":
			o[i] = c.NOPartitioning
		case "Ammoniapartitioning":
			o[i] = c.NHPartitioning
		case "Particlewetdeposition":
			o[i] = c.ParticleWetDep
		case "SO2wetdeposition":
			o[i] = c.SO2WetDep
		case "Non-SO2gaswetdeposition":
			o[i] = c.OtherGasWetDep
		case "Kxxyy":
			o[i] = c.Kxxyy
		case "Kzz":
			o[i] = c.Kzz
		case "M2u":
			o[i] = c.M2u
		case "M2d":
			o[i] = c.M2d
		case "PblTopLayer":
			o[i] = c.PblTopLayer
		default:
			panic(fmt.Sprintf("Unknown variable %v.", pol))
		}
		c.lock.RUnlock()
	}
	return o
}

func (d *InMAPdata) getGeometry(layer int) []geom.T {
	o := make([]geom.T, d.LayerEnd[layer]-d.LayerStart[layer])
	for i, c := range d.Data[d.LayerStart[layer]:d.LayerEnd[layer]] {
		o[i] = c.WebMapGeom
	}
	return o
}