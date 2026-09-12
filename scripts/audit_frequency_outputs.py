#!/usr/bin/env python3
"""Independent 2100 frequency audit using Decimal and original CSV/TXT files.

Generate all 21 scenarios with TestFrequencyMode_Real2100BWAudit first.
No production Go parser, matcher, or numeric conversion is imported here.
"""
import argparse
import collections
import csv
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
SCENARIOS = {
    'bw_0_0': (Decimal('0'), Decimal('0')),
    'bw_0.1_0.5': (Decimal('0.1'), Decimal('0.5')),
    'bw_2.5_5': (Decimal('2.5'), Decimal('5')),
    'bw_5_5': (Decimal('5'), Decimal('5')),
    'bw_10_20': (Decimal('10'), Decimal('20')),
    'bw_20_10': (Decimal('20'), Decimal('10')),
    'filters_off': (Decimal('5'), Decimal('5')),
}
NUMBER = r'-?\d+(?:[.,]\d+)?'
TERM = re.compile(r'"([^"]+)"\s*=\s*(' + NUMBER + r')(?:\s*-\s*(' + NUMBER + r'))?')


def number(value):
    return Decimal(value.strip().replace(',', '.'))


def load_rules(folder):
    result = []
    for path in sorted(folder.glob('*.txt')):
        content = path.read_text(encoding='utf-8-sig')
        assignments, conditions = content.split(';', 1)
        targets = collections.defaultdict(list)
        for field, low, high in TERM.findall(assignments):
            assert not high
            targets[field].append(number(low))
        groups = []
        for block in re.findall(r'\(([^()]*)\)', conditions):
            terms = [(field, number(low), number(high) if high else number(low))
                     for field, low, high in TERM.findall(block)]
            if terms:
                groups.append(terms)
        assert targets and groups, path
        result.append((path.name, targets, groups))
    assert len(result) == 4, folder
    return result


def evaluate(row, frequency, rules):
    """Return yes/no and an explanation of the independently selected rule."""
    candidates = []
    for name, assignments, groups in rules:
        for terms in groups:
            matches = True
            for field, low, high in terms:
                value = frequency if field == 'Frequency' else number(row[field])
                if not (value == low if low == high else low <= value < high):
                    matches = False
                    break
            if matches:
                candidates.append((-len(terms), name, assignments))
    if not candidates:
        return 'yes', 'no matching rule'
    _, name, assignments = min(candidates, key=lambda entry: entry[:2])
    same = all(number(row[field]) == value for field in ('MCC', 'MNC')
               for value in assignments.get(field, []))
    return ('yes' if same else 'no'), name


def audit(output_dir):
    common_rules = load_rules(ROOT / 'filters') + load_rules(ROOT / 'filtre_5G')
    rules = {'lte': common_rules, '5g': common_rules}
    exports = []
    needed = collections.defaultdict(set)
    for mode in ('segments', 'center', 'original'):
        for scenario in SCENARIOS:
            for tech in ('lte', '5g'):
                path = output_dir / mode / scenario / f'2100_frequencies_{tech}.csv'
                with path.open(newline='', encoding='utf-8') as stream:
                    assert stream.readline() == '\n', f'{path}: missing leading empty line'
                    reader = csv.DictReader(stream, delimiter=';')
                    assert 'Operator_sedi' in reader.fieldnames
                    assert 'Operator_sedi_BW' in reader.fieldnames
                    assert 'Operator_sedi_bV' not in reader.fieldnames
                    assert 'technologia' not in reader.fieldnames
                    rows = list(reader)
                assert rows, path
                exports.append((mode, scenario, tech, path, rows))
                for row in rows:
                    needed[Path(row['Zdrojovy_subor']).name].add(int(row['original_excel_row']))

    # Independently reread every input measurement, repair PLMN and keep the
    # source rows referenced by any export, for source/provenance verification.
    source_rows = {}
    input_counts = collections.Counter()
    raw_combinations = collections.Counter()
    corrected = 0
    for path in sorted((ROOT / 'data' / '2100').glob('*.csv')):
        tech = '5g' if '5G NR' in path.name else 'lte'
        with path.open(newline='', encoding='cp1250') as stream:
            for header_line, text in enumerate(stream, 1):
                if text.startswith('Date;Time;UTC;Latitude;Longitude;'):
                    header = next(csv.reader([text], delimiter=';'))
                    break
            else:
                raise AssertionError(f'{path}: no ROMES header')
            reader = csv.reader(stream, delimiter=';')
            for source_line, values in enumerate(reader, header_line + 1):
                if not values:
                    continue
                input_counts[tech] += 1
                row = dict(zip(header, values))
                plmns = [value for key, value in row.items() if 'PLMN' in key]
                plmns.extend(values[len(header):])
                mnc = row['MNC']
                replacements = {int(match.group(1)) for value in plmns
                                if (match := re.fullmatch(r'\s*\d{3}\s*/\s*(\d{1,3})\s*', value))}
                assert len(replacements) <= 1
                if replacements:
                    fixed = str(next(iter(replacements)))
                    if not mnc.strip() or number(mnc) != number(fixed):
                        corrected += 1
                    mnc = fixed
                row['MNC'] = mnc
                f = row['SSRef' if tech == '5g' else 'Frequency']
                if f and mnc and row['MCC']:
                    raw_combinations[(tech, row['MCC'], mnc, f)] += 1
                if source_line in needed[path.name]:
                    source_rows[path.name, source_line] = row
    assert corrected == 105403, corrected

    baseline = {}
    checked_rows = 0
    summaries = []
    examples = {}
    for mode, scenario, tech, path, rows in exports:
        counts = collections.Counter()
        bw = SCENARIOS[scenario][0 if tech == 'lte' else 1]
        active = [] if scenario == 'filters_off' else rules[tech]
        seen = set()
        for row in rows:
            source = source_rows[Path(row['Zdrojovy_subor']).name, int(row['original_excel_row'])]
            for field in ('MCC', 'MNC', 'PCI', 'Latitude', 'Longitude',
                          'SSS-RSRP' if tech == '5g' else 'RSRP',
                          'SSRef' if tech == '5g' else 'Frequency'):
                assert number(row[field]) == number(source[field]), (path, field, row[field], source[field])
            frequency = number(row['Frekvencia_Hz'])
            assert frequency == number(source['SSRef' if tech == '5g' else 'Frequency'])
            probes = [frequency, frequency - bw * 1_000_000, frequency + bw * 1_000_000]
            answers = [evaluate(row, probe, active) for probe in probes]
            expected = (answers[0][0], 'yes' if all(flag == 'yes' for flag, _ in answers) else 'no')
            actual = (row['Operator_sedi'], row['Operator_sedi_BW'])
            assert actual == expected, (path, row['original_excel_row'], expected, actual, probes, answers)
            zone = row['Usek' if mode == 'segments' else 'Zona']
            key = (zone, row['MCC'], row['MNC'], row['Frekvencia_Hz'])
            assert key not in seen, (path, 'duplicate', key)
            seen.add(key)
            unchanged = {k: v for k, v in row.items() if k not in ('Operator_sedi', 'Operator_sedi_BW')}
            baseline_key = (mode, tech, key)
            if scenario == 'bw_0_0':
                baseline[baseline_key] = unchanged
            else:
                assert unchanged == baseline[baseline_key], (path, 'BW changed measurement', key)
            counts['/'.join(actual)] += 1
            checked_rows += 1
            example_key = (tech, row['MNC'], row['Frekvencia_Hz'], str(bw), scenario == 'filters_off')
            if example_key not in examples:
                examples[example_key] = dict(technology=tech, mnc=row['MNC'], frequency_hz=str(frequency),
                    bw_mhz=str(bw), filters_off=scenario == 'filters_off', result='/'.join(actual),
                    probes=[dict(hz=str(probe), result=answer[0], rule=answer[1]) for probe, answer in zip(probes, answers)])
        expected_size = sum(key[0] == mode and key[1] == tech for key in baseline)
        assert len(rows) == expected_size
        summaries.append(dict(mode=mode, scenario=scenario, technology=tech, rows=len(rows), flags=dict(counts)))

    # Evaluate every original measurement too (identical keys share one exact
    # Decimal calculation). This audit is independent of strongest-row selection.
    raw_checks = 0
    raw_summary = []
    for scenario, widths in SCENARIOS.items():
        counts = collections.Counter()
        for (tech, mcc, mnc, f), count in raw_combinations.items():
            bw = widths[0 if tech == 'lte' else 1]
            active = [] if scenario == 'filters_off' else rules[tech]
            frequency = number(f)
            answers = [evaluate({'MCC': mcc, 'MNC': mnc}, point, active)[0]
                       for point in (frequency, frequency - bw*1_000_000, frequency + bw*1_000_000)]
            counts[tech + ':' + answers[0] + '/' + ('yes' if all(x == 'yes' for x in answers) else 'no')] += count
            raw_checks += count * 3
        raw_summary.append(dict(scenario=scenario, flags=dict(counts)))
    return dict(input_rows=dict(input_counts), corrected_mnc=corrected,
                export_files=len(exports), checked_export_rows=checked_rows,
                checked_export_probes=checked_rows*3, raw_measurement_probes=raw_checks,
                referenced_source_rows=len(source_rows), scenarios=summaries, examples=list(examples.values()),
                raw_scenarios=raw_summary,
                filter_sha256={str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
                               for folder in ('filters', 'filtre_5G') for p in sorted((ROOT/folder).glob('*.txt'))})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output_dir', type=Path)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    result = audit(args.output_dir)
    if args.report:
        args.report.write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
    print(json.dumps({key: result[key] for key in ('input_rows','corrected_mnc','export_files',
                     'checked_export_rows','checked_export_probes','raw_measurement_probes','referenced_source_rows')}, indent=2))
    print('PASS: exact Decimal oracle, source-row provenance, PLMN, unchanged measurements, all 21 scenarios')
