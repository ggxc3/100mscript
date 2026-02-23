package backend

import (
	"context"
	"fmt"
	"math"
)

type Point struct {
	A float64
	B float64
}

type PyProjTransformer struct {
	krovak *krovakEN
}

func NewPyProjTransformer() (*PyProjTransformer, error) {
	return &PyProjTransformer{krovak: newKrovakEN()}, nil
}

func (t *PyProjTransformer) Forward(ctx context.Context, lonLat []Point) ([]Point, error) {
	out := make([]Point, len(lonLat))
	for i, p := range lonLat {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		x, y, err := t.forwardOne(p.A, p.B)
		if err != nil {
			return nil, err
		}
		out[i] = Point{A: x, B: y}
	}
	return out, nil
}

func (t *PyProjTransformer) Inverse(ctx context.Context, xy []Point) ([]Point, error) {
	out := make([]Point, len(xy))
	for i, p := range xy {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		lon, lat, err := t.inverseOne(p.A, p.B)
		if err != nil {
			return nil, err
		}
		out[i] = Point{A: lon, B: lat}
	}
	return out, nil
}

func (t *PyProjTransformer) forwardOne(lonDeg, latDeg float64) (float64, float64, error) {
	lon := degToRad(lonDeg)
	lat := degToRad(latDeg)
	if math.IsNaN(lon) || math.IsNaN(lat) {
		return 0, 0, fmt.Errorf("invalid lon/lat")
	}

	// Match pyproj's default EPSG pipeline used by Transformer.from_crs(...):
	// axis order change + Inverse of "S-JTSK to WGS 84 (4)" + Krovak EN.
	x, y, z := geodeticToECEF(lon, lat, 0, ellipsoidWGS84)
	x, y, z = applyHelmertSJTSKtoWGS84_4(x, y, z, true)
	lonBessel, latBessel, _, err := ecefToGeodetic(x, y, z, ellipsoidBessel1841)
	if err != nil {
		return 0, 0, err
	}
	// Krovak core formula returns coordinates in the native Krovak axis order
	// (south/west-like ordering); EPSG:5514 expects East/North.
	pxK, pyK, err := t.krovak.forward(lonBessel, latBessel)
	if err != nil {
		return 0, 0, err
	}
	return pyK, pxK, nil
}

func (t *PyProjTransformer) inverseOne(x, y float64) (float64, float64, error) {
	lon, lat, err := t.inverseApproxOne(x, y)
	if err != nil {
		return 0, 0, err
	}
	// Refine against the forward transform. This makes inverse coordinates align
	// with PROJ/pyproj to sub-centimeter levels for our use cases while keeping
	// the runtime fully in Go.
	const stepDeg = 1e-6 // ~0.11 m in latitude; good finite-difference balance
	for i := 0; i < 8; i++ {
		fx, fy, err := t.forwardOne(lon, lat)
		if err != nil {
			return 0, 0, err
		}
		rx := x - fx
		ry := y - fy
		if math.Hypot(rx, ry) < 1e-6 {
			return lon, lat, nil
		}

		fxLon, fyLon, err := t.forwardOne(lon+stepDeg, lat)
		if err != nil {
			return 0, 0, err
		}
		fxLat, fyLat, err := t.forwardOne(lon, lat+stepDeg)
		if err != nil {
			return 0, 0, err
		}
		j11 := (fxLon - fx) / stepDeg
		j21 := (fyLon - fy) / stepDeg
		j12 := (fxLat - fx) / stepDeg
		j22 := (fyLat - fy) / stepDeg

		det := j11*j22 - j12*j21
		if math.Abs(det) < 1e-12 {
			break
		}
		dLon := (rx*j22 - ry*j12) / det
		dLat := (ry*j11 - rx*j21) / det
		lon += dLon
		lat += dLat
		if math.Abs(dLon)+math.Abs(dLat) < 1e-12 {
			break
		}
	}
	return lon, lat, nil
}

func (t *PyProjTransformer) inverseApproxOne(x, y float64) (float64, float64, error) {
	lonBessel, latBessel, err := t.krovak.inverse(y, x)
	if err != nil {
		return 0, 0, err
	}
	gx, gy, gz := geodeticToECEF(lonBessel, latBessel, 0, ellipsoidBessel1841)
	gx, gy, gz = applyHelmertSJTSKtoWGS84_4(gx, gy, gz, false)
	lon, lat, _, err := ecefToGeodetic(gx, gy, gz, ellipsoidWGS84)
	if err != nil {
		return 0, 0, err
	}
	return radToDeg(lon), radToDeg(lat), nil
}

// applyHelmertSJTSKtoWGS84_4 replicates PROJ's EPSG operation "S-JTSK to WGS 84 (4)"
// used by pyproj in this environment. Parameters are in position-vector convention.
// If inverse=true, applies the inverse transform (WGS84 geocentric -> Bessel geocentric).
func applyHelmertSJTSKtoWGS84_4(x, y, z float64, inverse bool) (float64, float64, float64) {
	const (
		tx = 485.0
		ty = 169.5
		tz = 483.8
		rx = 7.786 // arc-seconds
		ry = 4.398 // arc-seconds
		rz = 4.103 // arc-seconds
		s  = 0.0   // ppm
	)
	secToRad := math.Pi / (180.0 * 3600.0)
	rxr := rx * secToRad
	ryr := ry * secToRad
	rzr := rz * secToRad
	m := 1 + s*1e-6

	// Position-vector convention matrix (Bessel -> WGS84)
	a11, a12, a13 := m, -rzr, ryr
	a21, a22, a23 := rzr, m, -rxr
	a31, a32, a33 := -ryr, rxr, m

	if !inverse {
		x2 := tx + a11*x + a12*y + a13*z
		y2 := ty + a21*x + a22*y + a23*z
		z2 := tz + a31*x + a32*y + a33*z
		return x2, y2, z2
	}

	// Inverse: v1 = A^-1 * (v2 - t)
	bx := x - tx
	by := y - ty
	bz := z - tz
	det := a11*(a22*a33-a23*a32) - a12*(a21*a33-a23*a31) + a13*(a21*a32-a22*a31)
	if det == 0 {
		return x, y, z
	}
	inv11 := (a22*a33 - a23*a32) / det
	inv12 := (a13*a32 - a12*a33) / det
	inv13 := (a12*a23 - a13*a22) / det
	inv21 := (a23*a31 - a21*a33) / det
	inv22 := (a11*a33 - a13*a31) / det
	inv23 := (a13*a21 - a11*a23) / det
	inv31 := (a21*a32 - a22*a31) / det
	inv32 := (a12*a31 - a11*a32) / det
	inv33 := (a11*a22 - a12*a21) / det

	x1 := inv11*bx + inv12*by + inv13*bz
	y1 := inv21*bx + inv22*by + inv23*bz
	z1 := inv31*bx + inv32*by + inv33*bz
	return x1, y1, z1
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type ellipsoid struct {
	a  float64
	rf float64
	e2 float64
	e  float64
}

func newEllipsoid(a, rf float64) ellipsoid {
	f := 1.0 / rf
	e2 := 2*f - f*f
	return ellipsoid{a: a, rf: rf, e2: e2, e: math.Sqrt(e2)}
}

var (
	ellipsoidWGS84      = newEllipsoid(6378137.0, 298.257223563)
	ellipsoidBessel1841 = newEllipsoid(6377397.155, 299.1528128)
)

func geodeticToECEF(lon, lat, h float64, ell ellipsoid) (x, y, z float64) {
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)
	sinLon := math.Sin(lon)
	cosLon := math.Cos(lon)
	N := ell.a / math.Sqrt(1-ell.e2*sinLat*sinLat)
	x = (N + h) * cosLat * cosLon
	y = (N + h) * cosLat * sinLon
	z = (N*(1-ell.e2) + h) * sinLat
	return
}

func ecefToGeodetic(x, y, z float64, ell ellipsoid) (lon, lat, h float64, err error) {
	p := math.Hypot(x, y)
	if p == 0 {
		lon = 0
		if z > 0 {
			lat = math.Pi / 2
		} else if z < 0 {
			lat = -math.Pi / 2
		} else {
			lat = 0
		}
		h = math.Abs(z) - ell.a*math.Sqrt(1-ell.e2)
		return lon, lat, h, nil
	}

	lon = math.Atan2(y, x)
	f := 1.0 / ell.rf
	b := ell.a * (1 - f)
	ep2 := (ell.a*ell.a - b*b) / (b * b)
	theta := math.Atan2(z*ell.a, p*b)
	sinTheta := math.Sin(theta)
	cosTheta := math.Cos(theta)
	lat = math.Atan2(z+ep2*b*sinTheta*sinTheta*sinTheta, p-ell.e2*ell.a*cosTheta*cosTheta*cosTheta)
	// One Newton-like refinement for sub-mm numerical consistency.
	for i := 0; i < 3; i++ {
		sinLat := math.Sin(lat)
		N := ell.a / math.Sqrt(1-ell.e2*sinLat*sinLat)
		h = p/math.Cos(lat) - N
		newLat := math.Atan2(z, p*(1-ell.e2*(N/(N+h))))
		if math.Abs(newLat-lat) < 1e-14 {
			lat = newLat
			return lon, lat, h, nil
		}
		lat = newLat
	}
	return lon, lat, h, nil
}

type krovakEN struct {
	a     float64
	e     float64
	es    float64
	lat0  float64
	lon0  float64
	k0    float64
	alpha float64
	alfa  float64
	hae   float64
	k     float64
	k1    float64
	ro0   float64
	ad    float64
	s0    float64
	n     float64
	sinS0 float64
	cosS0 float64
	n0    float64
	uq    float64
	u0    float64
	g     float64
	czech bool
	x0    float64
	y0    float64
}

func newKrovakEN() *krovakEN {
	const (
		lat0Deg  = 49.5
		lon0Deg  = 24.8333333333333
		alphaDeg = 30.2881397527778
		k0       = 0.9999
		latTsDeg = 78.5
	)
	ell := ellipsoidBessel1841
	p := &krovakEN{
		a:     ell.a,
		e:     ell.e,
		es:    ell.e2,
		lat0:  degToRad(lat0Deg),
		lon0:  degToRad(lon0Deg),
		k0:    k0,
		alpha: degToRad(alphaDeg),
		s0:    degToRad(latTsDeg),
		czech: false,
		x0:    0,
		y0:    0,
	}
	p.init()
	return p
}

func (p *krovakEN) init() {
	const s45 = math.Pi / 4
	const s90 = math.Pi / 2
	sinLat0 := math.Sin(p.lat0)
	cosLat0 := math.Cos(p.lat0)
	p.alfa = math.Sqrt(1 + ((p.es * math.Pow(cosLat0, 4)) / (1 - p.es)))
	p.hae = p.alfa * p.e / 2
	p.uq = 1.04216856380474
	p.u0 = asinz(sinLat0 / p.alfa)
	p.g = math.Pow((1+p.e*sinLat0)/(1-p.e*sinLat0), p.alfa*p.e/2)
	p.k = math.Tan(p.u0/2+s45) / math.Pow(math.Tan(p.lat0/2+s45), p.alfa) * p.g
	p.k1 = p.k0
	p.sinS0 = math.Sin(p.s0)
	p.cosS0 = math.Cos(p.s0)
	p.n = p.sinS0
	p.n0 = p.a * math.Sqrt(1-p.es) / (1 - p.es*sinLat0*sinLat0)
	p.ro0 = p.k1 * p.n0 / math.Tan(p.s0)
	p.ad = s90 - p.uq
}

func (p *krovakEN) forward(lon, lat float64) (float64, float64, error) {
	const s45 = math.Pi / 4

	dlon := adjustLon(lon - p.lon0)
	gfi := math.Pow((1+p.e*math.Sin(lat))/(1-p.e*math.Sin(lat)), p.hae)
	u := 2 * (math.Atan(p.k*math.Pow(math.Tan(lat/2+s45), p.alfa)/gfi) - s45)
	deltav := -dlon * p.alfa
	s := asinz(math.Cos(p.ad)*math.Sin(u) + math.Sin(p.ad)*math.Cos(u)*math.Cos(deltav))
	d := asinz(math.Cos(u) * math.Sin(deltav) / math.Cos(s))
	eps := p.n * d
	ro := p.ro0 * math.Pow(math.Tan(p.s0/2+s45), p.n) / math.Pow(math.Tan(s/2+s45), p.n)

	x := ro*math.Cos(eps) + p.x0
	y := ro*math.Sin(eps) + p.y0
	if !p.czech {
		x = -x
		y = -y
	}
	return x, y, nil
}

func (p *krovakEN) inverse(x, y float64) (float64, float64, error) {
	const (
		s45     = math.Pi / 4
		maxIter = 15
		eps     = 1e-10
	)
	xt := x - p.x0
	yt := y - p.y0
	if !p.czech {
		xt = -xt
		yt = -yt
	}

	ro := math.Hypot(xt, yt)
	epsilon := math.Atan2(yt, xt)
	d := epsilon / p.sinS0
	s := 2 * (math.Atan(math.Pow(p.ro0/ro, 1/p.n)*math.Tan(p.s0/2+s45)) - s45)
	u := asinz(math.Cos(p.ad)*math.Sin(s) - math.Sin(p.ad)*math.Cos(s)*math.Cos(d))
	deltav := asinz(math.Cos(s) * math.Sin(d) / math.Cos(u))
	lon := p.lon0 - deltav/p.alfa

	lat := u
	for i := 0; i < maxIter; i++ {
		prev := lat
		sinPrev := math.Sin(prev)
		gfi := math.Pow((1+p.e*sinPrev)/(1-p.e*sinPrev), p.hae)
		lat = 2 * (math.Atan(math.Pow((1/p.k)*math.Tan(u/2+s45), 1/p.alfa)*gfi) - s45)
		if math.Abs(lat-prev) < eps {
			return adjustLon(lon), lat, nil
		}
	}
	return adjustLon(lon), lat, nil
}

func degToRad(v float64) float64 { return v * math.Pi / 180.0 }
func radToDeg(v float64) float64 { return v * 180.0 / math.Pi }

func adjustLon(lon float64) float64 {
	const twoPi = 2 * math.Pi
	for lon > math.Pi {
		lon -= twoPi
	}
	for lon < -math.Pi {
		lon += twoPi
	}
	return lon
}

func asinz(v float64) float64 {
	if v > 1 {
		v = 1
	}
	if v < -1 {
		v = -1
	}
	return math.Asin(v)
}
