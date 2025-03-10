# Dokumentácia CSV Zónového Analyzátora

## Obsah
1. [Úvod](#úvod)
2. [Základné koncepty](#základné-koncepty)
3. [Algoritmus spracovania dát](#algoritmus-spracovania-dát)
4. [Výpočty a matematické princípy](#výpočty-a-matematické-princípy)
5. [Práca so zónami](#práca-so-zónami)
6. [Výstupné dáta](#výstupné-dáta)
7. [Technické detaily](#technické-detaily)

## Úvod

CSV Zónový Analyzátor je program napísaný v TypeScripte, ktorý spracováva CSV súbory obsahujúce geografické dáta a RSRP (Reference Signal Received Power) merania. Program rozdeľuje merania do geografických zón definovanej veľkosti a počíta priemerné hodnoty RSRP pre každú zónu a MNC (Mobile Network Code).

Hlavným cieľom programu je zoskupiť blízke merania do zón, čo umožňuje lepšiu analýzu kvality signálu v konkrétnych geografických oblastiach.

## Základné koncepty

### Meranie (Measurement)

Meranie predstavuje jeden záznam z CSV súboru, ktorý obsahuje:
- Zemepisnú šírku (latitude)
- Zemepisnú dĺžku (longitude)
- MNC (Mobile Network Code) - identifikátor mobilnej siete
- RSRP (Reference Signal Received Power) - sila prijatého signálu
- Frekvenciu - frekvencia, na ktorej bolo meranie vykonané
- Pôvodný riadok - kompletné dáta z CSV súboru

### Zóna (Zone)

Zóna je geografická oblasť definovanej veľkosti (štandardne 100 metrov), ktorá obsahuje jedno alebo viac meraní. Zóna je identifikovaná:
- Súradnicami ľavého dolného rohu (latitude, longitude)
- MNC (Mobile Network Code)

Každá zóna obsahuje:
- Počet meraní v zóne
- Súčet RSRP hodnôt všetkých meraní v zóne
- Zoznam riadkov (indexov) z pôvodného CSV súboru
- Pôvodné dáta všetkých meraní v zóne
- Mapu frekvencií a ich počtov v zóne

### Kľúčové konštanty

- `ZONE_SIZE_METERS` - Veľkosť zóny v metroch (štandardne 100 metrov)
- `EARTH_RADIUS_METERS` - Polomer Zeme v metroch (6371000 metrov)
- `ZONE_SIZE_DEGREES` - Veľkosť zóny v stupňoch zemepisnej šírky/dĺžky (prepočítaná z metrov)
- `MAX_DIAGONAL_DISTANCE` - Maximálna vzdialenosť od stredu k rohu štvorcovej zóny

## Algoritmus spracovania dát

Proces spracovania dát prebieha v nasledujúcich krokoch:

1. **Načítanie CSV súboru**
   - Program načíta CSV súbor a rozdelí ho na riadky
   - Identifikuje hlavičku súboru (riadok obsahujúci "Date;Time;UTC;Latitude;Longitude")

2. **Definícia stĺpcov**
   - Používateľ môže použiť predvolené hodnoty stĺpcov alebo zadať vlastné
   - Program konvertuje písmená stĺpcov (A, B, C...) na indexy (0, 1, 2...)

3. **Spracovanie riadkov**
   - Pre každý riadok v definovanom rozsahu:
     - Parsuje meranie z riadku (latitude, longitude, MNC, RSRP, frekvencia)
     - Vypočíta súradnice zóny, do ktorej meranie patrí
     - Pridá meranie do existujúcej zóny alebo vytvorí novú zónu

4. **Uloženie výsledkov**
   - Program vytvorí nový CSV súbor s výsledkami
   - Pre každú zónu vytvorí jeden riadok s priemernými hodnotami

## Výpočty a matematické princípy

### Prevod geografických súradníc

Program pracuje s geografickými súradnicami (latitude, longitude) v stupňoch. Pre výpočty vzdialeností a definíciu zón je potrebné previesť stupne na metre.

- **Aproximácia**: 0.001 stupňa ≈ 111 metrov na rovníku
- **Výpočet veľkosti zóny v stupňoch**: `ZONE_SIZE_DEGREES = ZONE_SIZE_METERS / 111000`

### Výpočet vzdialenosti medzi bodmi

Na výpočet vzdialenosti medzi dvoma geografickými bodmi program používa Haversinovu formulu, ktorá zohľadňuje zakrivenie Zeme:

```typescript
function calculateDistance(point1: Coordinates, point2: Coordinates): number {
    const φ1 = (point1.latitude * Math.PI) / 180;
    const φ2 = (point2.latitude * Math.PI) / 180;
    const Δφ = ((point2.latitude - point1.latitude) * Math.PI) / 180;
    const Δλ = ((point2.longitude - point1.longitude) * Math.PI) / 180;

    const a =
        Math.sin(Δφ / 2) * Math.sin(Δφ / 2) +
        Math.cos(φ1) * Math.cos(φ2) * Math.sin(Δλ / 2) * Math.sin(Δλ / 2);
    const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));

    return EARTH_RADIUS_METERS * c;
}
```

### Určenie súradníc zóny

Pre každý bod (meranie) program určí, do ktorej zóny patrí, pomocou funkcie `getZoneCoordinates`:

```typescript
function getZoneCoordinates(point: Coordinates): Coordinates {
    return {
        latitude: Math.floor(point.latitude / ZONE_SIZE_DEGREES) * ZONE_SIZE_DEGREES,
        longitude: Math.floor(point.longitude / ZONE_SIZE_DEGREES) * ZONE_SIZE_DEGREES,
    };
}
```

Táto funkcia zaokrúhli súradnice bodu nadol na najbližší násobok veľkosti zóny v stupňoch, čím určí ľavý dolný roh zóny.

### Výpočet stredu zóny

Stred zóny sa vypočíta ako ľavý dolný roh zóny plus polovica veľkosti zóny:

```typescript
function getZoneCenter(zoneCoords: Coordinates): Coordinates {
    return {
        latitude: zoneCoords.latitude + ZONE_SIZE_DEGREES / 2,
        longitude: zoneCoords.longitude + ZONE_SIZE_DEGREES / 2,
    };
}
```

## Práca so zónami

### Identifikácia zón

Každá zóna je jednoznačne identifikovaná kľúčom, ktorý sa skladá z:
- Zemepisnej šírky ľavého dolného rohu zóny
- Zemepisnej dĺžky ľavého dolného rohu zóny
- MNC (Mobile Network Code)

Kľúč sa vytvára pomocou funkcie `createZoneKey`:

```typescript
function createZoneKey(coords: Coordinates, mnc: string): string {
    return `${coords.latitude},${coords.longitude},${mnc}`;
}
```

### Pridávanie meraní do zón

Pri spracovaní každého merania program:

1. Vypočíta súradnice zóny, do ktorej meranie patrí
2. Vytvorí kľúč zóny
3. Skontroluje, či zóna s týmto kľúčom už existuje:
   - Ak áno, pridá meranie do existujúcej zóny
   - Ak nie, skontroluje, či existuje blízka zóna s rovnakým MNC:
     - Ak áno, pridá meranie do najbližšej zóny
     - Ak nie, vytvorí novú zónu

### Hľadanie najbližšej zóny

Ak pre meranie neexistuje zóna s presne zodpovedajúcimi súradnicami, program hľadá najbližšiu zónu s rovnakým MNC v okruhu definovanej vzdialenosti (štandardne veľkosť zóny):

```typescript
function findNearestZone(
    point: Coordinates,
    mnc: string,
    existingZones: Map<string, Zone>,
    minDistance: number
): string | null {
    let nearestZoneKey: string | null = null;
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
```

## Výstupné dáta

Program vytvára nový CSV súbor s názvom `<pôvodný_súbor>_zones.csv`, ktorý obsahuje:
- Hlavičku z pôvodného súboru
- Jeden riadok pre každú zónu s:
  - Priemernými hodnotami RSRP
  - Najčastejšou frekvenciou v zóne
  - Informáciou o počte meraní v zóne
  - Zoznamom riadkov z pôvodného súboru, ktoré patria do zóny

Zóny sú zoradené podľa MNC, čo umožňuje ľahšie porovnanie medzi rôznymi mobilnými sieťami.

## Technické detaily

### Dátové štruktúry

Program používa nasledujúce hlavné dátové štruktúry:

1. **Measurement** - reprezentuje jedno meranie:
   ```typescript
   interface Measurement {
       latitude: number;
       longitude: number;
       mnc: string;
       rsrp: number;
       frequency: string;
       originalRow: string[];
   }
   ```

2. **Zone** - reprezentuje jednu zónu:
   ```typescript
   interface Zone {
       measurements: number;
       rsrpSum: number;
       rows: number[];
       originalData: string[][];
       frequencies: Map<string, number>;
   }
   ```

3. **Coordinates** - reprezentuje geografické súradnice:
   ```typescript
   interface Coordinates {
       latitude: number;
       longitude: number;
   }
   ```

### Optimalizácie

Program obsahuje niekoľko optimalizácií:

1. **Efektívne vyhľadávanie zón** - použitie Map pre rýchly prístup k zónam podľa kľúča
2. **Progresívne spracovanie** - zobrazenie progress baru počas spracovania
3. **Flexibilné nastavenia** - možnosť definovať vlastné stĺpce a rozsah riadkov
4. **Automatické zlučovanie blízkych meraní** - merania v blízkosti existujúcich zón sú zlúčené do týchto zón

### Obmedzenia

1. **Presnosť geografických výpočtov** - program používa aproximáciu pre prevod stupňov na metre, čo môže viesť k malým nepresnostiam
2. **Pamäťová náročnosť** - pri veľkých súboroch môže byť potrebné veľké množstvo pamäte
3. **Jednoduchý algoritmus zlučovania** - program zlučuje merania do najbližšej zóny s rovnakým MNC, ale nepoužíva pokročilé algoritmy klastrovania

### Možné vylepšenia

1. **Paralelné spracovanie** - využitie viacerých vlákien pre rýchlejšie spracovanie veľkých súborov
2. **Pokročilé algoritmy klastrovania** - implementácia sofistikovanejších algoritmov pre zlučovanie meraní
3. **Vizualizácia výsledkov** - pridanie možnosti vizualizovať zóny na mape
4. **Podpora viacerých formátov** - rozšírenie podpory pre iné formáty ako CSV 