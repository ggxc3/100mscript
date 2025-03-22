#!/bin/bash

# Testovací skript pre 100mscript
# Spustí všetky testovacie scenáre a zobrazí výsledky

echo "=== Spúšťam testy pre 100mscript ==="
echo ""

# Cesta k priečinku so scenármi
SCENARIOS_DIR="./test_data/scenarios"

# Zoznam testovacích súborov
TEST_FILES=(
  "test_scenarios.csv"
  "test_mcc.csv"
  "test_nearby.csv"
  "test_nearby_diff_mcc.csv"
)

# Spustenie testov
for test_file in "${TEST_FILES[@]}"; do
  echo "Spúšťam test: $test_file"
  # Automaticky odpovie "a" na otázku o použití predvolených hodnôt stĺpcov
  echo "a" | deno run --allow-read --allow-write main.ts "$SCENARIOS_DIR/$test_file"
  
  # Kontrola, či bol test úspešný
  if [ $? -eq 0 ]; then
    echo "✅ Test úspešne dokončený"
  else
    echo "❌ Test zlyhal"
  fi
  
  echo ""
done

echo "=== Všetky testy boli dokončené ==="
echo "Výsledky testov nájdete v priečinku $SCENARIOS_DIR"
echo "Súbory s výsledkami majú príponu _zones.csv" 