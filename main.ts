export interface Measurement {
	latitude: number;
	longitude: number;
	mnc: string;
	mcc: string;
	rsrp: number;
	sinr?: number;
	frequency: string;
	originalRow: string[];
}

export interface Zone {
	measurements: number;
	rsrpSum: number;
	sinrSum: number;
	sinrCount: number;
	rows: number[];
	originalData: string[][];
	frequencies: Map<string, number>;
}

export interface Coordinates {
	latitude: number;
	longitude: number;
}

/**
 * HLAVNÉ NASTAVENIA PROGRAMU
 * -------------------------------------------------------------------------
 * ZONE_SIZE_METERS - Definuje veľkosť zóny v metroch.
 * Zmena tejto hodnoty ovplyvní:
 * 1. Veľkosť geografických zón, do ktorých sa združujú merania
 * 2. Prepočet stupňov zemepisnej šírky/dĺžky na metre
 * 3. Maximálnu vzdialenosť bodu od stredu zóny
 *
 * Typické hodnoty:
 * - 50: Malé zóny pre hustejšie merania
 * - 100: Štandardné zóny (odporúčané)
 * - 200: Väčšie zóny pre riedke merania
 */
// Constants - hlavné nastavenia
export const ZONE_SIZE_METERS = 100; // Veľkosť zóny v metroch - toto môžete zmeniť
export const EARTH_RADIUS_METERS = 6371e3; // Polomer Zeme v metroch

// Odvodené konštanty - tieto sa prepočítajú automaticky
// 0.001 stupňa je približne 111 metrov na rovníku
export const ZONE_SIZE_DEGREES = ZONE_SIZE_METERS / 111000;
// Maximálna vzdialenosť od stredu k rohu štvorcovej zóny = √((ZONE_SIZE_METERS/2)² + (ZONE_SIZE_METERS/2)²)
export const MAX_DIAGONAL_DISTANCE = Math.sqrt(2) * (ZONE_SIZE_METERS / 2);

// Helper functions
export function columnLetterToIndex(letter: string): number {
	return letter.toUpperCase().charCodeAt(0) - 65;
}

export function calculateDistance(
	point1: Coordinates,
	point2: Coordinates
): number {
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

export function getZoneCoordinates(point: Coordinates): Coordinates {
	return {
		latitude:
			Math.floor(point.latitude / ZONE_SIZE_DEGREES) * ZONE_SIZE_DEGREES,
		longitude:
			Math.floor(point.longitude / ZONE_SIZE_DEGREES) * ZONE_SIZE_DEGREES,
	};
}

export function getZoneCenter(zoneCoords: Coordinates): Coordinates {
	return {
		latitude: zoneCoords.latitude + ZONE_SIZE_DEGREES / 2,
		longitude: zoneCoords.longitude + ZONE_SIZE_DEGREES / 2,
	};
}

export function createZoneKey(
	coords: Coordinates,
	mnc: string,
	mcc: string
): string {
	return `${coords.latitude},${coords.longitude},${mnc},${mcc}`;
}

export function parseMeasurement(
	row: string[],
	columns: number[]
): Measurement | null {
	try {
		const [latIndex, lonIndex, mncIndex, mccIndex, freqIndex, rsrpIndex, sinrIndex] = columns;
		const latitude = parseFloat(row[latIndex].replace(',', '.'));
		const longitude = parseFloat(row[lonIndex].replace(',', '.'));
		const rsrp = parseFloat(row[rsrpIndex].replace(',', '.'));
		const mnc = row[mncIndex]?.trim() || "";
		const mcc = row[mccIndex]?.trim() || "";

		if (isNaN(latitude) || isNaN(longitude) || isNaN(rsrp) || !mnc || !mcc) {
			return null;
		}

		let sinr = undefined;
		if (sinrIndex !== undefined && row[sinrIndex]) {
			const parsedSinr = parseFloat(row[sinrIndex].replace(',', '.'));
			if (!isNaN(parsedSinr)) {
				sinr = parsedSinr;
			}
		}

		return {
			latitude,
			longitude,
			mnc,
			mcc,
			frequency: row[freqIndex].trim(),
			rsrp,
			sinr,
			originalRow: [...row],
		};
	} catch {
		return null;
	}
}

export function findHeaderIndex(lines: string[]): number {
	return (
		lines.findIndex((line) =>
			line.includes('Date;Time;UTC;Latitude;Longitude')
		) || 0
	);
}

// Funkcia na vytvorenie progress baru
function createProgressBar(current: number, total: number, width = 40): string {
	const percentage = Math.round((current / total) * 100);
	const filledWidth = Math.round((current / total) * width);
	const emptyWidth = width - filledWidth;

	const filledBar = '█'.repeat(filledWidth);
	const emptyBar = '░'.repeat(emptyWidth);

	return `[${filledBar}${emptyBar}] ${current}/${total} (${percentage}%)`;
}

// Funkcia na aktualizáciu progress baru v konzole
function updateProgressBar(progressBar: string): void {
	// Vyčistíme aktuálny riadok a vypíšeme nový progress bar
	Deno.stdout.writeSync(new TextEncoder().encode('\r' + progressBar));
}

// Funkcia na nájdenie najbližšej zóny s rovnakým MNC a MCC v okruhu minDistance metrov
export function findNearestZone(
	point: Coordinates,
	mnc: string,
	mcc: string,
	existingZones: Map<string, Zone>,
	minDistance: number
): string | null {
	let nearestZoneKey: string | null = null;
	let minDistanceFound = Number.MAX_VALUE;
	
	for (const [key, _] of existingZones.entries()) {
		const [lat, lon, zoneMnc, zoneMcc] = key.split(',');
		
		// Kontrolujeme len zóny s rovnakým MNC a MCC
		if (zoneMnc === mnc && zoneMcc === mcc) {
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

export function processRows(
	rows: string[][],
	columns: number[],
	startRow: number,
	endRow: number,
	headerIndex: number
): Map<string, Zone> {
	const zones = new Map<string, Zone>();
	// Funkcia na prevod Excel riadku (1-indexed) na index v poli rows (0-indexed)
	// Príklad: Ak je hlavička na riadku 1, prvý dátový riadok je 2, a headerIndex = 0,
	// tak excelToArrayIndex(2) = 2 - (0 + 2) + 1 = 1, čo je index 1 v poli rows
	const excelToArrayIndex = (excelRow: number) => {
		// Upravená funkcia, ktorá správne mapuje Excel riadky na indexy v poli rows
		// Ak je hlavička na riadku 1 (headerIndex = 0), tak prvý dátový riadok je 2
		// a mal by sa mapovať na index 0 v poli rows
		return excelRow - (headerIndex + 2);
	};

	const totalRows = endRow - startRow + 1;
	let processedRows = 0;

	console.log('Spracovávam riadky...');
	updateProgressBar(createProgressBar(processedRows, totalRows));

	// Najprv zoradíme riadky podľa Excel riadku (aby sme spracovali riadky v správnom poradí)
	const rowsToProcess: number[] = [];
	for (let excelRow = startRow; excelRow <= endRow; excelRow++) {
		rowsToProcess.push(excelRow);
	}
	
	for (const excelRow of rowsToProcess) {
		const arrayIndex = excelToArrayIndex(excelRow);
		if (arrayIndex < 0 || arrayIndex >= rows.length) {
			processedRows++;
			updateProgressBar(createProgressBar(processedRows, totalRows));
			continue;
		}

		const measurement = parseMeasurement(rows[arrayIndex], columns);
		if (!measurement) {
			processedRows++;
			updateProgressBar(createProgressBar(processedRows, totalRows));
			continue;
		}

		const point = {
			latitude: measurement.latitude,
			longitude: measurement.longitude,
		};
		const zoneCoords = getZoneCoordinates(point);
		const zoneCenter = getZoneCenter(zoneCoords);
		const _distance = calculateDistance(point, zoneCenter);

		const zoneKey = createZoneKey(
			zoneCoords,
			measurement.mnc,
			measurement.mcc
		);
		
		// Ak zóna s týmto kľúčom už existuje, pridáme do nej meranie
		if (zones.has(zoneKey)) {
			const zone = zones.get(zoneKey)!;
			zone.measurements += 1;
			zone.rsrpSum += measurement.rsrp;
			
			if (measurement.sinr !== undefined) {
				zone.sinrSum += measurement.sinr;
				zone.sinrCount += 1;
			}
			
			zone.rows.push(excelRow);
			zone.originalData.push(measurement.originalRow);
			
			// Aktualizácia počtov frekvencií
			const currentFreqCount = zone.frequencies.get(measurement.frequency) || 0;
			zone.frequencies.set(measurement.frequency, currentFreqCount + 1);
			
			zones.set(zoneKey, zone);
		} else {
			// Ak zóna s týmto kľúčom neexistuje, skontrolujeme, či existuje blízka zóna s rovnakým MNC a MCC
			const nearestZoneKey = findNearestZone(point, measurement.mnc, measurement.mcc, zones, ZONE_SIZE_METERS);
			
			if (nearestZoneKey) {
				// Ak existuje blízka zóna s rovnakým MNC a MCC, pridáme meranie do nej
				const zone = zones.get(nearestZoneKey)!;
				zone.measurements += 1;
				zone.rsrpSum += measurement.rsrp;
				
				if (measurement.sinr !== undefined) {
					zone.sinrSum += measurement.sinr;
					zone.sinrCount += 1;
				}
				
				zone.rows.push(excelRow);
				zone.originalData.push(measurement.originalRow);
				
				// Aktualizácia počtov frekvencií
				const currentFreqCount = zone.frequencies.get(measurement.frequency) || 0;
				zone.frequencies.set(measurement.frequency, currentFreqCount + 1);
				
				zones.set(nearestZoneKey, zone);
			} else {
				// Ak neexistuje blízka zóna s rovnakým MNC a MCC, vytvoríme novú zónu
				const zone = {
					measurements: 1,
					rsrpSum: measurement.rsrp,
					sinrSum: measurement.sinr !== undefined ? measurement.sinr : 0,
					sinrCount: measurement.sinr !== undefined ? 1 : 0,
					rows: [excelRow],
					originalData: [measurement.originalRow],
					frequencies: new Map<string, number>()
				};
				
				// Inicializácia počtu frekvencií
				zone.frequencies.set(measurement.frequency, 1);
				
				zones.set(zoneKey, zone);
			}
		}

		processedRows++;
		updateProgressBar(createProgressBar(processedRows, totalRows));
	}

	console.log('\nSpracovanie dokončené!');

	return zones;
}

// Funkcia na uloženie výsledkov do CSV súboru
async function saveResultsToCSV(
	originalFilePath: string,
	zones: Map<string, Zone>,
	headerIndex: number,
	rsrpIndex: number,
	freqIndex: number,
	sinrIndex?: number
) {
	try {
		// Načítame pôvodný súbor
		const fileContent = await Deno.readTextFile(originalFilePath);
		const allLines = fileContent.split('\n');

		// Zachováme hlavičku
		const header = allLines[headerIndex];

		// Vytvoríme nový súbor s výsledkami - jeden riadok pre každú zónu
		const outputPath = originalFilePath.replace('.csv', '_zones.csv');

		// Zoradíme zóny podľa MNC a MCC
		const sortedZones = Array.from(zones.entries()).sort((a, b) => {
			const [, , mncA, mccA] = a[0].split(',');
			const [, , mncB, mccB] = b[0].split(',');

			// Zoradíme najprv podľa MCC, potom podľa MNC
			if (mccA !== mccB) {
				return parseInt(mccA) - parseInt(mccB);
			}
			return parseInt(mncA) - parseInt(mncB);
		});

		// Vytvoríme riadky pre každú zónu
		const zoneRows: string[] = [];

		// Pre každú zónu vytvoríme jeden riadok
		for (const [, zone] of sortedZones) {
			// Vezmeme prvý riadok z danej zóny ako základ
			const baseRow = [...zone.originalData[0]];

			// Vypočítame priemernú hodnotu RSRP pre zónu
			const averageRSRP = zone.rsrpSum / zone.measurements;

			// Aktualizujeme RSRP hodnotu na priemer zóny
			baseRow[rsrpIndex] = averageRSRP.toFixed(2);
			
			if (sinrIndex !== undefined && zone.sinrCount > 0) {
				const averageSINR = zone.sinrSum / zone.sinrCount;
				baseRow[sinrIndex] = averageSINR.toFixed(2);
			}

			// Nájdeme najčastejšiu frekvenciu v zóne
			let mostFrequentFrequency = "";
			let maxCount = 0;

			for (const [freq, count] of zone.frequencies.entries()) {
				if (count > maxCount) {
					maxCount = count;
					mostFrequentFrequency = freq;
				}
			}

			// Aktualizujeme frekvenciu na najčastejšiu hodnotu v zóne
			baseRow[freqIndex] = mostFrequentFrequency;

			// Pridáme informáciu o počte meraní a riadkoch
			const rowInfo = `# Meraní: ${
				zone.measurements
			}, Excel riadky: ${zone.rows.join(',')}`;

			// Vytvoríme riadok pre zónu
			const zoneRow = baseRow.join(';') + ` ${rowInfo}`;
			zoneRows.push(zoneRow);
		}

		// Spojíme hlavičku a riadky zón - pridáme prázdny riadok pred hlavičku
		const zoneContent = ['', header, ...zoneRows].join('\n');

		// Zapíšeme výsledky do súboru
		await Deno.writeTextFile(outputPath, zoneContent);

		console.log(`Výsledky zón uložené do súboru ${outputPath}`);
	} catch (error) {
		console.error('Chyba pri ukladaní výsledkov:', error);
	}
}

async function main() {
	let filePath = '';

	// Skontrolujeme, či bol zadaný parameter pri spustení programu
	if (Deno.args.length >= 1) {
		filePath = Deno.args[0];
	} else {
		// Ak nebol zadaný parameter, požiadame používateľa o zadanie cesty k súboru
		filePath = prompt('Zadajte cestu k CSV súboru:') || '';
		if (!filePath) {
			console.error('Nebola zadaná cesta k súboru.');
			Deno.exit(1);
		}
	}

	try {
		const fileContent = await Deno.readTextFile(filePath);
		const allLines = fileContent.split('\n');
		const headerIndex = findHeaderIndex(allLines);
		
		// Predvolené hodnoty stĺpcov
		const defaultColumnLetters = {
			latitude: 'D',
			longitude: 'E',
			frequency: 'K',
			mnc: 'N',
			mcc: 'M',
			rsrp: 'W',
			sinr: 'V'
		};
		
		// Zobrazíme predvolené hodnoty a opýtame sa používateľa, či ich chce použiť
		console.log('Predvolené hodnoty stĺpcov:');
		console.log(`  Zemepisná šírka (latitude): ${defaultColumnLetters.latitude}`);
		console.log(`  Zemepisná dĺžka (longitude): ${defaultColumnLetters.longitude}`);
		console.log(`  Frekvencia (frequency): ${defaultColumnLetters.frequency}`);
		console.log(`  MNC: ${defaultColumnLetters.mnc}`);
		console.log(`  MCC: ${defaultColumnLetters.mcc}`);
		console.log(`  RSRP: ${defaultColumnLetters.rsrp}`);
		console.log(`  SINR: ${defaultColumnLetters.sinr}`);
		
		const useDefaultColumns = prompt('Chcete použiť predvolené hodnoty stĺpcov? (a/n):')?.toLowerCase() === 'a';
		
		let columns: number[];
		let startRow: number;
		let endRow: number;
		
		// Process data
		const dataContent = allLines.slice(headerIndex);
		const rows = dataContent
			.slice(1)
			.map((line) => line.trim())
			.filter((line) => line.length > 0)
			.map((line) => line.split(';'));
		
		if (useDefaultColumns) {
			// Ak používateľ vybral predvolené hodnoty, použijeme predvolené stĺpce
			// a spracujeme celý súbor automaticky
			columns = [
				columnLetterToIndex(defaultColumnLetters.latitude),
				columnLetterToIndex(defaultColumnLetters.longitude),
				columnLetterToIndex(defaultColumnLetters.mnc),
				columnLetterToIndex(defaultColumnLetters.mcc),
				columnLetterToIndex(defaultColumnLetters.frequency),
				columnLetterToIndex(defaultColumnLetters.rsrp),
				columnLetterToIndex(defaultColumnLetters.sinr),
			];
			
			// Nastavíme rozsah na všetky riadky (od hlavičky po koniec súboru)
			startRow = headerIndex + 2; // Prvý riadok dát (hlavička + 1)
			endRow = headerIndex + 1 + rows.length; // Posledný riadok dát
			
			console.log(`Celkový počet riadkov v súbore (bez hlavičky): ${rows.length}`);
			console.log(`Spracovanie všetkých riadkov od hlavičky až po koniec súboru`);
		} else {
			// Ak používateľ chce zadať vlastné hodnoty stĺpcov
			columns = [
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre zemepisnú šírku (latitude):') || defaultColumnLetters.latitude
				),
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre zemepisnú dĺžku (longitude):') || defaultColumnLetters.longitude
				),
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre MNC:') || defaultColumnLetters.mnc
				),
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre MCC:') || defaultColumnLetters.mcc
				),
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre frekvenciu:') || defaultColumnLetters.frequency
				),
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre RSRP:') || defaultColumnLetters.rsrp
				),
				columnLetterToIndex(
					prompt('Zadajte písmeno stĺpca pre SINR:') || defaultColumnLetters.sinr
				),
			];
			
			// Umožníme používateľovi zvoliť rozsah riadkov
			console.log(`Celkový počet riadkov v súbore (bez hlavičky): ${rows.length}`);
			const range = prompt('Zadajte rozsah riadkov pre spracovanie (napr. 1-1000):') || '';
			
			if (range && range.includes('-')) {
				// Ak používateľ zadal platný rozsah, použijeme ho
				const [start, end] = range.split('-').map((n) => parseInt(n.trim()));
				startRow = start || (headerIndex + 2);
				endRow = end || (headerIndex + 1 + rows.length);
			} else {
				// Ak nie je zadaný platný rozsah, vezmeme všetky riadky
				startRow = headerIndex + 2; // Prvý riadok dát (hlavička + 1)
				endRow = headerIndex + 1 + rows.length; // Posledný riadok dát
				console.log('Nebol zadaný platný rozsah, spracúvajú sa všetky riadky.');
			}
			
			console.log(`Spracovanie riadkov ${startRow} až ${endRow}`);
		}
		
		console.log(
			`Indexy stĺpcov [lat,lon,mnc,mcc,freq,rsrp,sinr]: ${columns.join(',')}`
		);

		const zones = processRows(rows, columns, startRow, endRow, headerIndex);

		// Uložíme výsledky do CSV súboru
		await saveResultsToCSV(filePath, zones, headerIndex, columns[5], columns[4], columns[6]);

		console.log('Spracovanie úspešne dokončené!');
	} catch (error: unknown) {
		console.error(
			'Chyba pri čítaní súboru:',
			error instanceof Error ? error.message : 'Neznáma chyba'
		);
		Deno.exit(1);
	}
}

if (import.meta.main) {
	main();
}
