import {
	assertEquals,
	assertAlmostEquals,
} from 'https://deno.land/std@0.208.0/assert/mod.ts';

// Import functions from main.ts
import {
	columnLetterToIndex,
	calculateDistance,
	getZoneCoordinates,
	parseMeasurement,
	processRows,
	createZoneKey,
} from './main.ts';

Deno.test('columnLetterToIndex converts Excel column letters correctly', () => {
	assertEquals(columnLetterToIndex('A'), 0);
	assertEquals(columnLetterToIndex('B'), 1);
	assertEquals(columnLetterToIndex('Z'), 25);
	assertEquals(columnLetterToIndex('a'), 0); // should handle lowercase
});

Deno.test('calculateDistance calculates correct distances', () => {
	const point1 = { latitude: 48.1234, longitude: 17.1234 };
	const point2 = { latitude: 48.1235, longitude: 17.1235 };

	const distance = calculateDistance(point1, point2);
	// Približne 13.4 metrov medzi týmito bodmi (podľa výstupu z hlavného programu)
	assertAlmostEquals(distance, 13.4, 0.1);
});

Deno.test('getZoneCoordinates returns correct zone coordinates', () => {
	const point = { latitude: 48.123456, longitude: 17.123456 };
	const zone = getZoneCoordinates(point);

	assertEquals(zone, {
		latitude: 48.123,
		longitude: 17.123,
	});
});

Deno.test('parseMeasurement parses valid data correctly', () => {
	const row = ['48,1234', '17,1234', '1', '-85,5'];
	const columns = [0, 1, 2, 3];

	const measurement = parseMeasurement(row, columns);
	assertEquals(measurement, {
		latitude: 48.1234,
		longitude: 17.1234,
		mnc: '1',
		rsrp: -85.5,
	});
});

Deno.test('parseMeasurement returns null for invalid data', () => {
	const row = ['invalid', '17,1234', '1', '-85,5'];
	const columns = [0, 1, 2, 3];

	const measurement = parseMeasurement(row, columns);
	assertEquals(measurement, null);
});

Deno.test('processRows processes data correctly', () => {
	const rows = [
		['48,1234', '17,1234', '1', '-85,5'],
		['48,1234', '17,1234', '1', '-86,5'],
		['48,1234', '17,1234', '2', '-87,5'],
	];
	const columns = [0, 1, 2, 3];
	const startRow = 1;
	const endRow = 3;
	const headerIndex = 0;

	const zones = processRows(rows, columns, startRow, endRow, headerIndex);

	// Kontrola, že máme dve zóny (pre MNC 1 a 2)
	assertEquals(zones.size, 2);

	// Kontrola prvej zóny (MNC 1)
	const zoneCoords = getZoneCoordinates({
		latitude: 48.1234,
		longitude: 17.1234,
	});
	const zoneKey1 = createZoneKey(zoneCoords, '1');
	const zone1 = zones.get(zoneKey1);

	assertEquals(zone1?.measurements, 2);
	assertEquals(zone1?.rsrpSum, -172);
	assertEquals(zone1?.rows, [1, 2]);

	// Kontrola druhej zóny (MNC 2)
	const zoneKey2 = createZoneKey(zoneCoords, '2');
	const zone2 = zones.get(zoneKey2);

	assertEquals(zone2?.measurements, 1);
	assertEquals(zone2?.rsrpSum, -87.5);
	assertEquals(zone2?.rows, [3]);
});
