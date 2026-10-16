# Wymagania v1 i macierz odbioru — PLAT-9 / T01

Data przeglądu: 2026-10-16. Źródło zakresu: PLAT-9 oraz
[audyt i backlog](project-audit.md). Dokument definiuje docelowy odbiór;
nie potwierdza wdrożenia platformy ani zamknięcia zadań implementacyjnych.

## Baza i drift

| Repozytorium | Baza zadania | Sprawdzony checkout |
| --- | --- | --- |
| CLI | `ec7320b7c6c503751aefa996c49e760473964412` | `cb9d4439aca5d792d024ecd51f696e26a5aa96b7` |
| templates (odczyt `/templates`) | `5da82328723c62e97efe4256f490a232b1dc1c08` | `fa43e1ac2c6f07b62fdfb0c265489097ea0a17e9` |

Sprawdzono lokalne historie i diff obu baz do checkoutów, bez dostępu do sieci.
Lokalny `main` CLI wskazuje `51da2453df12f4945c7544d53d29b189c68449f6`;
HEAD dodaje dokumentację pilota. Aktualność zdalnego main nie jest potwierdzona.
Od bazy CLI dodano m.in. blokadę całego cyklu mutacji, zatwierdzanie niezmiennego
preview v2 i regresje tych mechanizmów. Historyczne luki PLAT-12/13 w audycie
nie opisują już w pełni bieżącego kodu. W templates zmieniły się dokumentacja
i proces; trzy manifesty nie zmieniły się względem bazy.

Przegląd obejmuje `cmd/{init,preflight,plan,apply,status,destroy,workflow}.go`,
testy CLI i procesu, snapshot silnika oraz `tools/check-templates/main.go`.
`plan` renderuje i zapisuje lokalny preview; nie wykonuje `steps.plan` ani
`terraform plan`. `status` czyta lokalny checkpoint, bez badania klastra.
Pin i testy z fixture nie są dowodem Kind/EKS E2E.

## Zakres i poziomy odbioru

**P — portfolio (lokalne v1):** działające demo od czystej instalacji na
izolowanym Kind: lifecycle CLI, GitOps przez Argo CD, aplikacja przez ingress,
monitoring i alert, awaria/resume, kontrolowany cleanup oraz instrukcja demo.
Samo renderowanie YAML lub kroki `echo` nie spełniają tego poziomu.

**E — projekt inżynierski:** cały poziom P oraz EKS, zabezpieczenie stanu i danych,
koszty i teardown, porównawcze eksperymenty oraz opracowanie z traceability.
AWS jest jedynym targetem cloud v1. Wymagania akademickie wymagają osobnego
potwierdzenia według rejestru poniżej; ten zakres techniczny ich nie zastępuje.

„P+E” w macierzy oznacza wymaganie wspólne. E dziedziczy także wszystkie wiersze P.
Realizacja PLAT-9 dostarcza specyfikację i scenariusze, bez implementacji innych
zadań, zmian bramek CI, wydania ani uruchamiania integracji.

## Macierz wymaganie → test → dowód

F oznacza wymaganie funkcjonalne, Q jakościowe. T-* to identyfikatory scenariuszy
odbioru, nie deklaracja istnienia automatycznych testów. Każdy wiersz wymaga
protokołu z wynikiem i artefaktami wskazanymi w ostatniej kolumnie.

| ID / poziom / zadania | Wymaganie i mierzalne kryterium odbioru | Test odbiorczy | Wymagany dowód |
| --- | --- | --- | --- |
| F-01 / P+E / PLAT-16,17,24 | Init z przypiętego szablonu tworzy poprawną konfigurację; brakujące/błędne wartości lub niezaufane źródło są odrzucone bez częściowej konfiguracji. | T-01: poprawny i błędny manifest/input, przerwanie init, zmiana digestu źródła. | Konfiguracja bez sekretów, pin/digest, exit codes i porównanie plików przed/po. |
| F-02 / P+E / PLAT-20 | Preflight wykrywa brak narzędzia, błędną wersję, niedostępny Docker, błędne credentials i cel; strict kończy się błędem; każda kontrola ma skończony zadeklarowany timeout. | T-02: osobna kontrolowana awaria każdego warunku, także zawieszona kontrola i niewłaściwy kontekst. | Lista wersji, zanonimizowane wyniki kontroli, czas i kod wyjścia; cel testowy. |
| F-03 / P+E / PLAT-13,14 | Plan pokazuje kroki i digest powiązany z bajtami źródła/wyniku, wejściami i celem; nie wykonuje kroków infrastruktury ani instalacji narzędzi. Zmieniony preview blokuje wykonanie. | T-03: render, powtórzenie bez zmian, zmiana pliku/inputu/celu, próba apply/resume. | Digests, diff generated i stan przed/po, log odmowy; jawne oznaczenie „CLI preview”. |
| F-04 / P+E / PLAT-21,26 | Apply/destroy wymagają zgody; odmowa/EOF nie zmieniają generated ani checkpointów. Resume nie powtarza ukończonych kroków i wymaga tej samej zatwierdzonej fazy i treści. Wynik niepewny wymaga inspekcji przed ponowieniem. | T-04: odmowa, EOF, awaria kroku, resume obu faz, drift i ponowne apply. | Log kroków/ID, diff stanu i plików, inspekcja zasobów po awarii. |
| F-05 / P / PLAT-25,27,30 | Kind udostępnia demo przez ingress; Argo CD synchronizuje zadeklarowaną rewizję, a zmiana rewizji zmienia odpowiedź aplikacji. | T-05: czysta instalacja → apply → readiness → HTTP → zmiana GitOps → HTTP. | Rewizje GitOps, Synced/Healthy, nodes/pods Ready i odpowiedzi HTTP 200 z oczekiwaną wersją; nie tylko status CLI. |
| F-06 / P / PLAT-28 | Monitoring zbiera metrykę demo; kontrolowana awaria wyzwala alert, a przywrócenie usuwa alarm w zadeklarowanych oknach czasowych. | T-06: zdrowa aplikacja, awaria, odtworzenie; porównanie z regułą alertu. | Zapytanie i próbki metryki, dashboard, reguła z oknami i znaczniki firing/resolved. |
| F-07 / P+E / PLAT-22,23 | Status odróżnia checkpoint lokalny od aktualnego zdrowia; błąd API lub brak dostępu daje unknown/error, nigdy pozorne healthy. JSON i błędy mają udokumentowany kontrakt. | T-07: zdrowy i uszkodzony klaster, niedostępne API, brak state; parsowanie JSON stdout i kodów błędów. | Wyniki CLI wraz z niezależnym odczytem klastra i opisem kontraktu. |
| F-08 / E / PLAT-31,32,33 | EKS powstaje z zatwierdzonego zapisanego planu Terraform w uzgodnionym koncie/regionie; chroniony backend stanu i dostęp operatora działają, demo jest osiągalne. | T-08: przegląd planfile → apply tego pliku → readiness/HTTP → kontrolowana próba odzyskania workspace. | Identyfikator konta/region, digest planfile, pin modułów, konfiguracja backendu bez sekretów i protokół dostępu/odtworzenia. |
| F-09 / E / PLAT-34,35 | Teardown EKS usuwa wszystkie własne zasoby rozliczane; zachowanie danych jest uzgodnione przed testem. Koszt mieści się w zatwierdzonym budżecie i TTL. | T-09: inwentaryzacja przed/po destroy, przerwanie i resume cleanup, kontrola kosztów po opóźnieniu rozliczeń. | Lista własnych LB, wolumenów, NAT, EC2, EKS i pozostałości, decyzja o danych, koszt z datą i zakresem; brak raportu billing oznacza wynik oczekujący. |
| Q-01 / P+E / PLAT-12,14,15,26 | Mutacje w workspace są serializowane, state zapisywany atomowo, pliki obce i zasoby innych zadań zachowane; ścieżka/symlink nie pozwala pisać poza dozwolony obszar. | T-10: dwa procesy, crash, awaria renderowania, kolizja i symlink oraz obcy zasób kontrolny. | Wyniki regresji procesu, sumy plików przed/po, lista zasobów kontrolnych. |
| Q-02 / P+E / PLAT-17,18,25,36 | Odtwarzalność: wydanie CLI z checksumą i SHA, jawna para CLI–template, piny narzędzi/chartów i brak kolizji wersji między projektami. | T-11: dwie czyste instalacje tej samej pary i dwa projekty z różnymi wersjami narzędzia; błędna checksuma. | Macierz faktycznie sprawdzonych OS/arch/wersji, checksums, piny i porównanie wyników instalacji. Nie deklarować wsparcia nieprzetestowanych platform. |
| Q-03 / P+E / PLAT-19,29 | Sekrety nie trafiają do publicznych logów/artefaktów; RBAC i izolacja namespace blokują obcy dostęp. Szablon jest zaufanym kodem, command+args nie jest sandboxem. | T-12: syntetyczny sekret w stdout/stderr, planie/JSON/błędzie; pozytywna i negatywna próba dostępu między namespace. | Zredagowane logi, wyniki wyszukania znacznika testowego i odmowy RBAC; bez prawdziwych credentials. |
| Q-04 / P / PLAT-30,37 | Demo daje się odtworzyć z instrukcji dwukrotnie w osobnych czystych środowiskach; po destroy nie ma własnych zasobów, obce pozostają. | T-13: dwa pełne przebiegi T-05/06/04 i cleanup wykonane według instrukcji. | Wersje, czasy etapów, wyniki obu prób, inwentaryzacja przed/po i materiały demo. |
| Q-05 / E / PLAT-38 | Porównanie CLI z ręcznym bootstrapem używa tego samego stacku i zasobów, obejmuje ≥3 próby każdej metody, czas do readiness, kroki ręczne, błędy i koszt. Nie zakłada przewagi CLI. | T-14: ustalenie protokołu przed pomiarem, identyczne punkty start/stop, rejestr wszystkich prób i odchyleń. | Protokół, surowe dane, skrypt analizy, mediana i rozrzut, ograniczenia oraz wyniki negatywne. Próg ≥3 to minimum techniczne, nie wymóg uczelni. |
| Q-06 / E / PLAT-39,40 | Opracowanie łączy wymagania z architekturą, implementacją, eksperymentami i wnioskami; spełnia potwierdzone wytyczne akademickie. | T-15: przegląd śledzenia każdego ID oraz osobny przegląd wymagań A-01–A-05. | Wersja opracowania, macierz rozdziałów/dowodów i potwierdzenie promotora; otwarte A-* blokują odbiór akademicki. |
| Q-07 / P+E / PLAT-10,11,36,41 | Wydanie ma zielone wymagane kontrole i niezależne review dokładnego SHA, zasady licencji/zależności, instrukcję migracji i rollback bez automatycznego cofania infrastruktury. | T-16: przegląd pakietu wydania, kontroli i review, próba powrotu binarki w nieaktywnym testowym workspace z zachowaniem stanu. | Linki review/CI i SHA, artefakt, licencja, instrukcja odzyskania; sam rollback binarki nie jest rollbackiem zasobów. |

## Stan dowodów i protokół odbioru

W bieżącym kodzie istnieją regresje wspierające T-03/04/10, m.in.
`TestApprovedPreviewRejectsDriftBeforeMutation`,
`TestApprovalRejectionPreservesFilesAndDoesNotInstall`,
`TestPreviewIsNotApprovalAndResumeRequiresSamePhase` w
[approval_test.go](../cmd/approval_test.go) oraz
`TestWorkflowsRejectConcurrentMutations` i
`TestCrashRetainsLockUntilSurvivingStepExits` w
[workflow_process_test.go](../cmd/workflow_process_test.go).
To częściowe dowody zachowania silnika, nie pełny odbiór tych wymagań.
[check-templates](../tools/check-templates/main.go) waliduje i renderuje manifesty
bez wykonania kroków. Pozostałe scenariusze opisane powyżej wymagają zebrania
dowodów; nie mają w tym dokumencie statusu PASS. W szczególności T-05/06/08/09/13/14/15
nie zostały wykonane w ramach PLAT-9.

Protokół każdej próby musi zawierać: ID wymagania i testu, datę, wykonawcę,
pełne SHA CLI/templates, wersje narzędzi i środowiska, warunki początkowe,
komendy, oczekiwany i rzeczywisty wynik, link/ścieżkę do dowodu bez sekretów,
wynik PASS/FAIL/BLOCKED/NOT RUN, pozostałości oraz decyzję odbierającego.
NOT RUN i brak dowodu nie oznaczają PASS. Po zmianie pary SHA dowody wymagają
oceny aktualności i ponowienia testów dotkniętego zachowania.

Odbiór P wymaga PASS wszystkich wierszy P i P+E; odbiór E dodatkowo wszystkich
wierszy E i zamknięcia A-01–A-05. Znane FAIL/BLOCKED lub brak dowodu zatrzymują
odpowiedni odbiór. Właściciel projektu zatwierdza zakres i odbiór techniczny,
operator zbiera dowody runtime po autoryzacji, promotor/uczelnia potwierdza
wymagania i odbiór akademicki. Dokument nie stanowi ich podpisu.

## Wymagania uczelni i promotora — rejestr do potwierdzenia

Źródła dostępne dla PLAT-9 nie zawierają wytycznych uczelni ani uzgodnień
promotora. Nie przyjęto domyślnych wymagań za potwierdzone. Poniższe pozycje
są pytaniami do właściciela projektu/promotora, a nie ustaleniami uczelni.

| ID | Informacja do zebrania | Źródło / status | Warunek zamknięcia |
| --- | --- | --- | --- |
| A-01 | Uczelnia, kierunek, regulamin i obowiązujący szablon pracy | Brak dostarczonego źródła; otwarte | Link lub dokument z wersją/datą i potwierdzenie zastosowania. |
| A-02 | Zatwierdzony temat, cel, pytania badawcze i wymagany wkład własny | Brak ustaleń promotora; otwarte | Treść uzgodnienia, osoba potwierdzająca i data. |
| A-03 | Wymagana metoda, liczba prób i kryteria oceny eksperymentów | Brak ustaleń promotora; otwarte | Akceptacja protokołu T-14 lub zapis wymaganych zmian. |
| A-04 | Forma/objętość pracy, język, cytowania, zasady użycia AI, licencja i publikacja kodu/danych | Brak dostarczonego źródła; otwarte | Konkretne wytyczne i potwierdzenie promotora; bez zgadywania limitów. |
| A-05 | Terminy, wymagane artefakty, prezentacja/demo i kryteria obrony | Brak dostarczonego źródła; otwarte | Harmonogram i lista kryteriów z autorytatywnym źródłem. |

Brak tych danych nie blokuje lokalnego demo ani przygotowania dokumentacji
PLAT-9, ale blokuje deklarację kompletności wymagań i odbioru akademickiego.
Właściciel projektu dostarcza źródła; następna rewizja wiąże każde potwierdzone
ustalenie z testem i dowodem, zachowując historię decyzji.

## Poza v1 i warunki operatora

- Azure/AKS: wyłącznie reference i statyczne renderowanie; bez obietnicy
  wspieranego lifecycle, E2E czy równoważności z AWS. GCP i inne chmury poza v1.
- Produkcja, HA/SLA, multi-region, platforma wielodostępna i zarządzana usługa
  nie są celem demo. Izolacja i RBAC z Q-03 nadal obowiązują.
- Własny silnik IaC lub CI/CD, zastąpienie Terraform/Helm/kubectl oraz sandbox
  dla dowolnego niezaufanego kodu szablonów są poza zakresem.
- Migracja istniejących klastrów, automatyczny restore danych, destrukcyjna
  naprawa i stałe utrzymywanie płatnego klastra są poza zakresem.
- Pełne EKS, eksperymenty i opracowanie należą do E, nie są wymagane do P.

Cloud apply/destroy wymaga osobnego jawnego zakresu: konto, region, zasoby,
budżet, TTL i sposób postępowania z danymi. Lokalny runtime wymaga izolowanego
klastra/workspace/kubeconfig. W tym checkoutcie operator hosta wykonuje commit,
niezależne review SHA, zewnętrzne CI i publikację; PLAT-9 nie zmienia tych bramek.
Brak środowiska runtime nie może zostać opisany jako sukces E2E.
