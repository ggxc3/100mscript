# Načítanie a spájanie CSV – 22. 9. 2026

## Príčina a oprava

Náhľad hlavičiek načítaval všetky riadky všetkých súborov, vytváral spojenú kópiu a potom každý súbor načítal druhýkrát kvôli jeho samostatnej schéme. Pridanie ďalšieho súboru zopakovalo celý postup. Staré asynchrónne požiadavky pokračovali aj po zmene výberu. To spôsobovalo zbytočné alokácie, čítanie disku a riziko prekročenia päťminútového limitu náhľadu.

Náhľad teraz číta najviac 1 MiB na súbor a spája iba názvy stĺpcov. Cache uchováva len hlavičky, kontroluje veľkosť a čas úpravy súboru a po úspešnom načítaní odstraňuje nevybrané súbory. Novšia požiadavka zruší staršiu; čítania sú serializované. Frekvenčné a PLMN mapovanie sa počas obnovovania neresetuje. Spracovanie naďalej číta celý obsah a objaví aj neskoré dodatočné stĺpce/PLMN. Ručný zoznam stĺpcov v náhľade obsahuje pomenovanú hlavičku a dodatočné stĺpce z úvodnej vzorky.

CSV parser spracuje bežné neúvodzovkované riadky bez vytvárania nového CSV čítača pre každý riadok a každé meranie rozdelí iba raz. Úvodzovkované polia vrátane bodkočiarok naďalej používa štandardný CSV parser. Zarovnanie pri spájaní počíta indexy stĺpcov raz pre súbor. Frekvenčný režim obmedzuje kopírovanie riadkov a používa rovnaké stabilné časové radenie ako pôvodný režim. Nemenná časová zóna Europe/Bratislava sa načíta raz namiesto opakovaného čítania pri každom časovom údaji.

## Merania na rovnakom počítači

Vstup: pôvodné `data/2100`, približne 290 MiB, 566 566 5G a 419 998 LTE riadkov. Porovnanie s commitom `78b77eb`. Časy nie sú garanciou pre iný počítač alebo sieťový disk. Alokácie znamenajú kumulatívne pridelenú pamäť počas operácie, nie maximálnu spotrebu RAM.

| Operácia | Pred opravou | Po oprave |
|---|---:|---:|
| Prvý náhľad oboch súborov | 9,15 s | 0,011 s |
| Alokácie prvého náhľadu | 39 895 MiB | 22,2 MiB |
| Spracovanie 11 častí, úseky | 8,31 s | 3,90 s |
| Spracovanie 11 častí, štvorce so stredom | 6,85 s | 4,01 s |
| Spracovanie 11 častí, štvorce s prvým bodom | 7,32 s | 3,31 s |
| Alokácie pri 11 častiach | 24 413–24 572 MiB | 6 402–6 561 MiB |

## Overenie

- Reálne dáta rozdelené bez straty riadkov do 11 súborov: šesť 5G a päť LTE, najviac 100 000 riadkov na časť. V každom priestorovom režime sa zachovalo všetkých 678 520 priestorovo použiteľných meraní a 105 403 opráv MNC.
- Štvorcové zóny: celé a rozdelené súbory majú identické skupiny, maximá RSRP, počty meraní a príznaky operátora.
- Úseky: používa sa spoločné geometrické priraďovanie samostatných zdrojových trás ako v pôvodnom režime. Rozdelenie na nové zdrojové trasy môže ovplyvniť hranice úsekov; test netvrdí identitu hraníc s jedným súvislým CSV. Samostatný regresný test porovnáva všetky tri priestorové režimy s pôvodným mechanizmom na rovnakých rozdelených vstupoch vrátane obráteného poradia a nezoradených časov.
- Hlavičky: pevný limit prečítaných bajtov, CP1250/UTF-8, preambuly, úvodzovky, bodkočiarky v poliach, dodatočné stĺpce vrátane PLMN až za limitom náhľadu, invalidácia cache a obnovenie po zrušení.
- Prehliadač: postupné pridanie skutočných súborov cez lokálne Go načítanie hlavičiek; `SSRef` a `Frequency` sa zobrazili, PLMN a technológia sa zachovali. Overené zotavenie po neexistujúcom súbore bez reštartu aj odmietnutie oneskoreného náhľadu po zmene zoznamu. Výberové dialógy a Wails eventový most boli simulované.
- Celý existujúci audit BW: 21 spracovaní, 42 CSV, 74 060 výsledných riadkov, kontrola samostatným Decimal orákulom.
- Bežné testy, `go test -race ./...`, `go vet ./...`, frontendový build.

## Reprodukcia

```sh
RUN_LARGE_REAL_DATA_TESTS=1 go test . -run '^TestPreviewReal2100Performance$' -count=1 -v
RUN_LARGE_REAL_DATA_TESTS=1 go test ./internal/backend -run '^TestFrequencyMode_Real2100SplitFiles$' -count=1 -v -timeout 10m
```

Testy reálnych dát sú voliteľné; vyžadujú lokálne pôvodné CSV. Zákazníkove konkrétne súbory pri oprave neboli dostupné.
