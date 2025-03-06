# CSV Zónový Analyzátor

Tento skript spracováva CSV súbory s geografickými dátami a RSRP meraniami. Rozdeľuje merania do 100-metrových zón a počíta priemerné hodnoty RSRP pre každú zónu a MNC.

## Požiadavky

- [Deno](https://deno.land/) runtime

## Použitie

```bash
deno run --allow-read main.ts <cesta_k_csv_suboru>
```

Program vás požiada o zadanie:
1. Rozsahu riadkov na spracovanie (napr. "1-100")
2. Písmeno stĺpca pre latitude
3. Písmeno stĺpca pre longitude
4. Písmeno stĺpca pre MNC
5. Písmeno stĺpca pre RSRP

## Výstup

Program vytvorí analýzu priemerných hodnôt RSRP pre každú 100-metrovú zónu, rozdelenú podľa MNC. 