export interface Measurement {
	latitude: number;
	longitude: number;
	mnc: string;
	rsrp: number;
	frequency: string;
	originalRow: string[];
}

export interface Zone {
	measurements: number;
	rsrpSum: number;
	rows: number[];
	originalData: string[][];
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
	frequency: string
): string {
	return `${coords.latitude},${coords.longitude},${mnc},${frequency}`;
}

export function parseMeasurement(
	row: string[],
	columns: number[]
): Measurement | null {
	try {
		const [latIndex, lonIndex, mncIndex, freqIndex, rsrpIndex] = columns;
		const latitude = parseFloat(row[latIndex].replace(',', '.'));
		const longitude = parseFloat(row[lonIndex].replace(',', '.'));
		const rsrp = parseFloat(row[rsrpIndex].replace(',', '.'));

		if (isNaN(latitude) || isNaN(longitude) || isNaN(rsrp)) {
			return null;
		}

		return {
			latitude,
			longitude,
			mnc: row[mncIndex].trim(),
			frequency: row[freqIndex].trim(),
			rsrp,
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

export function processRows(
	rows: string[][],
	columns: number[],
	startRow: number,
	endRow: number,
	headerIndex: number
): Map<string, Zone> {
	const zones = new Map<string, Zone>();
	const excelToArrayIndex = (excelRow: number) =>
		excelRow - (headerIndex + 2) + 1;

	const totalRows = endRow - startRow + 1;
	let processedRows = 0;

	console.log('Spracovávam riadky...');
	updateProgressBar(createProgressBar(processedRows, totalRows));

	for (let excelRow = startRow; excelRow <= endRow; excelRow++) {
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
		const distance = calculateDistance(point, zoneCenter);

		const zoneKey = createZoneKey(
			zoneCoords,
			measurement.mnc,
			measurement.frequency
		);
		const zone = zones.get(zoneKey) || {
			measurements: 0,
			rsrpSum: 0,
			rows: [],
			originalData: [],
		};
		zone.measurements += 1;
		zone.rsrpSum += measurement.rsrp;
		zone.rows.push(excelRow);
		zone.originalData.push(measurement.originalRow);
		zones.set(zoneKey, zone);

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
	rsrpIndex: number
) {
	try {
		// Načítame pôvodný súbor
		const fileContent = await Deno.readTextFile(originalFilePath);
		const allLines = fileContent.split('\n');

		// Zachováme hlavičku
		const header = allLines[headerIndex];

		// Vytvoríme nový súbor s výsledkami - jeden riadok pre každú zónu
		const outputPath = originalFilePath.replace('.csv', '_zones.csv');

		// Zoradíme zóny podľa MNC a frekvencie
		const sortedZones = Array.from(zones.entries()).sort((a, b) => {
			const [, , mncA, freqA] = a[0].split(',');
			const [, , mncB, freqB] = b[0].split(',');

			// Najprv zoradíme podľa MNC
			const mncCompare = parseInt(mncA) - parseInt(mncB);
			if (mncCompare !== 0) return mncCompare;

			// Ak je MNC rovnaké, zoradíme podľa frekvencie
			return parseInt(freqA) - parseInt(freqB);
		});

		// Vytvoríme riadky pre každú zónu
		const zoneRows: string[] = [];

		// Pre každú zónu vytvoríme jeden riadok
		for (const [_, zone] of sortedZones) {
			// Vezmeme prvý riadok z danej zóny ako základ
			const baseRow = [...zone.originalData[0]];

			// Vypočítame priemernú hodnotu RSRP pre zónu
			const averageRSRP = zone.rsrpSum / zone.measurements;

			// Aktualizujeme RSRP hodnotu na priemer zóny
			baseRow[rsrpIndex] = averageRSRP.toFixed(2);

			// Pridáme informáciu o počte meraní a riadkoch
			const rowInfo = `# Measurements: ${
				zone.measurements
			}, Excel rows: ${zone.rows.join(',')}`;

			// Vytvoríme riadok pre zónu
			const zoneRow = baseRow.join(';') + ` ${rowInfo}`;
			zoneRows.push(zoneRow);
		}

		// Spojíme hlavičku a riadky zón
		const zoneContent = [header, ...zoneRows].join('\n');

		// Zapíšeme výsledky do súboru
		await Deno.writeTextFile(outputPath, zoneContent);

		console.log(`Zone results saved to ${outputPath}`);
	} catch (error) {
		console.error('Error saving results:', error);
	}
}

async function main() {
	let filePath = '';

	// Skontrolujeme, či bol zadaný parameter pri spustení programu
	if (Deno.args.length >= 1) {
		filePath = Deno.args[0];
	} else {
		// Ak nebol zadaný parameter, požiadame používateľa o zadanie cesty k súboru
		filePath = prompt('Enter CSV file path:') || '';
		if (!filePath) {
			console.error('No file path provided.');
			Deno.exit(1);
		}
	}

	try {
		const fileContent = await Deno.readTextFile(filePath);
		const allLines = fileContent.split('\n');
		const headerIndex = findHeaderIndex(allLines);

		// Get user input
		const range = prompt('Enter row range (e.g., 1-100):') || '';
		const [startRow, endRow] = range.split('-').map((n) => parseInt(n));

		const columns = [
			columnLetterToIndex(
				prompt('Enter column letter for latitude:') || ''
			),
			columnLetterToIndex(
				prompt('Enter column letter for longitude:') || ''
			),
			columnLetterToIndex(prompt('Enter column letter for MNC:') || ''),
			columnLetterToIndex(
				prompt('Enter column letter for frequency:') || ''
			),
			columnLetterToIndex(prompt('Enter column letter for RSRP:') || ''),
		];

		// Process data
		const dataContent = allLines.slice(headerIndex);
		const rows = dataContent
			.slice(1)
			.map((line) => line.trim())
			.filter((line) => line.length > 0)
			.map((line) => line.split(';'));

		console.log(`Total rows in file (excluding header): ${rows.length}`);
		console.log(`Processing rows ${startRow} to ${endRow} (Excel)`);
		console.log(
			`Column indices [lat,lon,mnc,freq,rsrp]: ${columns.join(',')}`
		);

		const zones = processRows(rows, columns, startRow, endRow, headerIndex);

		// Uložíme výsledky do CSV súboru
		await saveResultsToCSV(filePath, zones, headerIndex, columns[4]);

		console.log('Spracovanie úspešne dokončené!');
	} catch (error: unknown) {
		console.error(
			'Error reading file:',
			error instanceof Error ? error.message : 'Unknown error'
		);
		Deno.exit(1);
	}
}

if (import.meta.main) {
	main();
}
