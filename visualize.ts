import {
  Measurement,
  Zone,
  Coordinates,
  ZONE_SIZE_METERS,
  ZONE_SIZE_DEGREES,
  calculateDistance,
  getZoneCoordinates,
  getZoneCenter,
  createZoneKey,
  findNearestZone
} from './main.ts';

// Vytvoríme HTML súbor s vizualizáciou
async function createVisualizationHtml() {
  const html = `<!DOCTYPE html>
<html>
<head>
  <title>Vizualizácia algoritmu zónovania</title>
  <style>
    body {
      font-family: Arial, sans-serif;
      margin: 20px;
    }
    canvas {
      border: 1px solid #ccc;
      display: block;
      margin: 0 auto;
    }
    .controls {
      margin: 10px;
      text-align: center;
    }
    button {
      margin: 0 5px;
      padding: 5px 10px;
    }
    .info {
      margin: 20px;
      max-width: 800px;
      margin: 0 auto;
    }
    .color-legend {
      display: flex;
      flex-wrap: wrap;
      justify-content: center;
      margin: 10px 0;
    }
    .color-item {
      margin: 5px 10px;
      display: flex;
      align-items: center;
    }
    .color-box {
      width: 20px;
      height: 20px;
      margin-right: 5px;
      border: 1px solid #ccc;
    }
  </style>
</head>
<body>
  <h1>Vizualizácia algoritmu zónovania</h1>
  
  <div class="info">
    <h3>Ako funguje algoritmus zónovania</h3>
    <p>Tento nástroj vizualizuje, ako algoritmus rozdeľuje geografické merania do zón.</p>
    <p>Veľkosť zóny: <strong>${ZONE_SIZE_METERS} metrov</strong></p>
    
    <div class="color-legend">
      <h4>Farby reprezentujú rôzne MNC (Mobile Network Code):</h4>
      <div class="color-items">
        <div class="color-item">
          <div class="color-box" style="background-color: #FF5733"></div>
          <span>MNC 1</span>
        </div>
        <div class="color-item">
          <div class="color-box" style="background-color: #33FF57"></div>
          <span>MNC 2</span>
        </div>
        <div class="color-item">
          <div class="color-box" style="background-color: #3357FF"></div>
          <span>MNC 3</span>
        </div>
        <div class="color-item">
          <div class="color-box" style="background-color: #FF33F5"></div>
          <span>MNC 4</span>
        </div>
        <div class="color-item">
          <div class="color-box" style="background-color: #F5FF33"></div>
          <span>MNC 5</span>
        </div>
      </div>
    </div>
    
    <p><strong>Princíp algoritmu:</strong></p>
    <ol>
      <li>Body sa priraďujú do zón na základe ich geografických súradníc a MNC.</li>
      <li>Ak je bod blízko existujúcej zóny s rovnakým MNC, priradí sa do nej namiesto vytvorenia novej zóny.</li>
      <li>Pre každú zónu sa vypočíta priemerná hodnota RSRP a určí sa najčastejšia frekvencia.</li>
    </ol>
  </div>
  
  <canvas id="visualization" width="800" height="600"></canvas>
  
  <div class="controls">
    <button id="start-btn">Spustiť animáciu</button>
    <button id="reset-btn">Reset</button>
    <button id="step-btn">Krok po kroku</button>
    <label>
      Rýchlosť:
      <input type="range" id="speed-slider" min="50" max="1000" value="500">
    </label>
    <label>
      Počet bodov:
      <input type="number" id="points-input" min="10" max="500" value="100">
    </label>
  </div>
  
  <script>
    // Konštanty pre vizualizáciu
    const CANVAS_WIDTH = 800;
    const CANVAS_HEIGHT = 600;
    const POINT_RADIUS = 3;
    const ZONE_BORDER_WIDTH = 1;
    let ANIMATION_SPEED = 500;
    
    // Farby pre rôzne MNC
    const MNC_COLORS = {
      '1': '#FF5733', // Oranžová
      '2': '#33FF57', // Zelená
      '3': '#3357FF', // Modrá
      '4': '#FF33F5', // Ružová
      '5': '#F5FF33'  // Žltá
    };
    
    // Konštanty pre algoritmus zónovania
    const ZONE_SIZE_METERS = ${ZONE_SIZE_METERS};
    const ZONE_SIZE_DEGREES = ${ZONE_SIZE_DEGREES};
    
    // Funkcia na výpočet vzdialenosti medzi dvoma bodmi
    function calculateDistance(point1, point2) {
      const φ1 = (point1.latitude * Math.PI) / 180;
      const φ2 = (point2.latitude * Math.PI) / 180;
      const Δφ = ((point2.latitude - point1.latitude) * Math.PI) / 180;
      const Δλ = ((point2.longitude - point1.longitude) * Math.PI) / 180;
    
      const a =
        Math.sin(Δφ / 2) * Math.sin(Δφ / 2) +
        Math.cos(φ1) * Math.cos(φ2) * Math.sin(Δλ / 2) * Math.sin(Δλ / 2);
      const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
    
      return 6371e3 * c; // Polomer Zeme v metroch
    }
    
    // Funkcia na získanie súradníc zóny
    function getZoneCoordinates(point) {
      return {
        latitude: Math.floor(point.latitude / ZONE_SIZE_DEGREES) * ZONE_SIZE_DEGREES,
        longitude: Math.floor(point.longitude / ZONE_SIZE_DEGREES) * ZONE_SIZE_DEGREES,
      };
    }
    
    // Funkcia na získanie stredu zóny
    function getZoneCenter(zoneCoords) {
      return {
        latitude: zoneCoords.latitude + ZONE_SIZE_DEGREES / 2,
        longitude: zoneCoords.longitude + ZONE_SIZE_DEGREES / 2,
      };
    }
    
    // Funkcia na vytvorenie kľúča zóny
    function createZoneKey(coords, mnc) {
      return \`\${coords.latitude},\${coords.longitude},\${mnc}\`;
    }
    
    // Funkcia na nájdenie najbližšej zóny
    function findNearestZone(point, mnc, existingZones, minDistance) {
      let nearestZoneKey = null;
      let minDistanceFound = Number.MAX_VALUE;
      
      for (const [key, _] of existingZones.entries()) {
        const [lat, lon, zoneMnc] = key.split(',');
        
        // Kontrolujeme len zóny s rovnakým MNC
        if (zoneMnc === mnc) {
          const existingZoneCenter = {
            latitude: parseFloat(lat) + ZONE_SIZE_DEGREES / 2,
            longitude: parseFloat(lon) + ZONE_SIZE_DEGREES / 2
          };
          
          const distance = calculateDistance(point, existingZoneCenter);
          
          // Ak je vzdialenosť menšia ako minimálna požadovaná a menšia ako doteraz nájdená minimálna vzdialenosť
          if (distance < minDistance && distance < minDistanceFound) {
            minDistanceFound = distance;
            nearestZoneKey = key;
          }
        }
      }
      
      return nearestZoneKey;
    }
    
    // Funkcia na generovanie náhodných bodov
    function generateRandomPoints(centerLat, centerLon, radiusKm, count) {
      const points = [];
      const zones = new Map();
      
      // Konverzia km na stupne (približne)
      const radiusDegrees = radiusKm / 111;
      
      for (let i = 0; i < count; i++) {
        // Generujeme náhodný bod v kruhu
        const r = radiusDegrees * Math.sqrt(Math.random());
        const theta = Math.random() * 2 * Math.PI;
        
        const latitude = centerLat + r * Math.cos(theta);
        const longitude = centerLon + r * Math.sin(theta);
        
        // Náhodné MNC (1-5)
        const mnc = String(Math.floor(Math.random() * 5) + 1);
        
        points.push({
          coordinates: { latitude, longitude },
          mnc,
          zoneKey: null,
          processed: false
        });
      }
      
      return {
        points,
        zones,
        currentStep: 0,
        totalSteps: count
      };
    }
    
    // Funkcia na vykreslenie vizualizácie
    function drawVisualization(data) {
      const canvas = document.getElementById('visualization');
      const ctx = canvas.getContext('2d');
      ctx.clearRect(0, 0, CANVAS_WIDTH, CANVAS_HEIGHT);
      
      // Nájdeme minimálne a maximálne súradnice pre škálovanie
      let minLat = Infinity, maxLat = -Infinity;
      let minLon = Infinity, maxLon = -Infinity;
      
      for (const point of data.points) {
        minLat = Math.min(minLat, point.coordinates.latitude);
        maxLat = Math.max(maxLat, point.coordinates.latitude);
        minLon = Math.min(minLon, point.coordinates.longitude);
        maxLon = Math.max(maxLon, point.coordinates.longitude);
      }
      
      // Pridáme malý okraj
      const latMargin = (maxLat - minLat) * 0.1;
      const lonMargin = (maxLon - minLon) * 0.1;
      
      minLat -= latMargin;
      maxLat += latMargin;
      minLon -= lonMargin;
      maxLon += lonMargin;
      
      // Funkcia na konverziu geografických súradníc na súradnice canvasu
      const geoToCanvas = (lat, lon) => {
        const x = ((lon - minLon) / (maxLon - minLon)) * CANVAS_WIDTH;
        const y = CANVAS_HEIGHT - ((lat - minLat) / (maxLat - minLat)) * CANVAS_HEIGHT;
        return { x, y };
      };
      
      // Vykreslíme zóny
      for (const [key, zone] of data.zones.entries()) {
        const [lat, lon, mnc] = key.split(',');
        const zoneCoords = {
          latitude: parseFloat(lat),
          longitude: parseFloat(lon)
        };
        
        // Vypočítame rohy zóny
        const topLeft = geoToCanvas(
          zoneCoords.latitude,
          zoneCoords.longitude
        );
        const bottomRight = geoToCanvas(
          zoneCoords.latitude + ZONE_SIZE_DEGREES,
          zoneCoords.longitude + ZONE_SIZE_DEGREES
        );
        
        const width = bottomRight.x - topLeft.x;
        const height = bottomRight.y - topLeft.y;
        
        // Vykreslíme zónu
        ctx.fillStyle = zone.color + '40'; // Pridáme priehľadnosť
        ctx.fillRect(topLeft.x, topLeft.y, width, height);
        
        // Vykreslíme okraj zóny
        ctx.strokeStyle = zone.color;
        ctx.lineWidth = ZONE_BORDER_WIDTH;
        ctx.strokeRect(topLeft.x, topLeft.y, width, height);
        
        // Vypíšeme informácie o zóne
        ctx.fillStyle = '#000000';
        ctx.font = '10px Arial';
        ctx.fillText(\`MNC: \${mnc}, Meraní: \${zone.measurements}\`, topLeft.x + 5, topLeft.y + 15);
      }
      
      // Vykreslíme body
      for (const point of data.points) {
        const { x, y } = geoToCanvas(point.coordinates.latitude, point.coordinates.longitude);
        
        // Farba bodu podľa MNC
        const color = MNC_COLORS[point.mnc] || '#000000';
        
        // Vykreslíme bod
        ctx.beginPath();
        ctx.arc(x, y, POINT_RADIUS, 0, 2 * Math.PI);
        
        if (point.processed) {
          // Spracované body sú plné
          ctx.fillStyle = color;
          ctx.fill();
        } else {
          // Nespracované body majú len okraj
          ctx.strokeStyle = color;
          ctx.lineWidth = 1;
          ctx.stroke();
        }
      }
      
      // Vypíšeme aktuálny krok
      ctx.fillStyle = '#000000';
      ctx.font = '14px Arial';
      ctx.fillText(\`Krok: \${data.currentStep} / \${data.totalSteps}\`, 10, 20);
    }
    
    // Funkcia na spracovanie jedného bodu
    function processNextPoint(data) {
      if (data.currentStep >= data.totalSteps) {
        return false; // Všetky body sú spracované
      }
      
      const point = data.points[data.currentStep];
      const zoneCoords = getZoneCoordinates(point.coordinates);
      const zoneKey = createZoneKey(zoneCoords, point.mnc);
      
      // Skontrolujeme, či zóna už existuje
      if (data.zones.has(zoneKey)) {
        // Ak áno, pridáme bod do existujúcej zóny
        const zone = data.zones.get(zoneKey);
        zone.measurements += 1;
        zone.rsrpSum += Math.random() * 10; // Náhodná RSRP hodnota
        data.zones.set(zoneKey, zone);
      } else {
        // Ak nie, skontrolujeme, či existuje blízka zóna s rovnakým MNC
        const nearestZoneKey = findNearestZone(
          point.coordinates,
          point.mnc,
          data.zones,
          ZONE_SIZE_METERS
        );
        
        if (nearestZoneKey) {
          // Ak existuje blízka zóna, pridáme bod do nej
          const zone = data.zones.get(nearestZoneKey);
          zone.measurements += 1;
          zone.rsrpSum += Math.random() * 10;
          data.zones.set(nearestZoneKey, zone);
          point.zoneKey = nearestZoneKey;
        } else {
          // Ak neexistuje blízka zóna, vytvoríme novú
          const newZone = {
            measurements: 1,
            rsrpSum: Math.random() * 10,
            rows: [data.currentStep],
            originalData: [[]],
            frequencies: new Map(),
            color: MNC_COLORS[point.mnc] || '#000000'
          };
          
          data.zones.set(zoneKey, newZone);
          point.zoneKey = zoneKey;
        }
      }
      
      // Označíme bod ako spracovaný
      point.processed = true;
      data.currentStep++;
      
      return true; // Pokračujeme v spracovaní
    }
    
    // Funkcia na spustenie animácie
    function startAnimation(data) {
      const animationStep = () => {
        if (processNextPoint(data)) {
          drawVisualization(data);
          setTimeout(animationStep, ANIMATION_SPEED);
        } else {
          console.log('Animácia dokončená');
        }
      };
      
      animationStep();
    }
    
    // Inicializácia po načítaní stránky
    window.onload = function() {
      // Získame referencie na ovládacie prvky
      const startButton = document.getElementById('start-btn');
      const resetButton = document.getElementById('reset-btn');
      const stepButton = document.getElementById('step-btn');
      const speedSlider = document.getElementById('speed-slider');
      const pointsInput = document.getElementById('points-input');
      
      // Generujeme náhodné dáta
      let data = generateRandomPoints(48.1485, 17.1077, 1, 100); // Bratislava
      
      // Vykreslíme počiatočný stav
      drawVisualization(data);
      
      // Nastavíme event listenery
      startButton.addEventListener('click', () => {
        startAnimation(data);
      });
      
      resetButton.addEventListener('click', () => {
        const pointCount = parseInt(pointsInput.value) || 100;
        data = generateRandomPoints(48.1485, 17.1077, 1, pointCount);
        drawVisualization(data);
      });
      
      stepButton.addEventListener('click', () => {
        if (processNextPoint(data)) {
          drawVisualization(data);
        }
      });
      
      speedSlider.addEventListener('input', () => {
        ANIMATION_SPEED = parseInt(speedSlider.value);
      });
      
      pointsInput.addEventListener('change', () => {
        const pointCount = parseInt(pointsInput.value) || 100;
        data = generateRandomPoints(48.1485, 17.1077, 1, pointCount);
        drawVisualization(data);
      });
    };
  </script>
</body>
</html>`;

  await Deno.writeTextFile("visualization.html", html);
  console.log("HTML súbor vytvorený: visualization.html");
  console.log("Otvorte tento súbor vo vašom prehliadači pre zobrazenie vizualizácie.");
}

// Hlavná funkcia
async function main() {
  await createVisualizationHtml();
}

// Spustíme hlavnú funkciu, ak je tento súbor spustený priamo
if (import.meta.main) {
  main();
} 