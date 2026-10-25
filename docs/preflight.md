# Preflight, tożsamość i bootstrap

```sh
platformctl preflight --strict --phase bootstrap
platformctl preflight --strict --phase apply
platformctl preflight --strict --phase destroy
```

Preflight sprawdza zainstalowane narzędzia, credentials z manifestu, Docker
(jeśli wymagany), zapis workspace i poprawność lokalnego stanu. Nie instaluje
narzędzi, nie tworzy katalogu stanu i usuwa krótkie próby zapisu. Bootstrap nie
wymaga istniejącego klastra; apply/destroy sprawdzają kontekst, gdy ich kroki
go używają. Apply/destroy przygotowują zarządzane narzędzia, następnie wykonują
strict checks przed generowaniem i krokami workflow.

Proces diagnostyczny ma domyślnie 5 s i łączny limit 32 KiB stdout/stderr.
`--check-timeout 100ms..30s` zmienia deadline standalone preflight; cały przebieg
ma 2 min. Timeout, błąd uruchomienia, zbyt duży output i błąd kontroli wersji
są nieudaną wymaganą kontrolą. Strict zwraca kod 1. Nonstrict może zwrócić kod 0
z `ok: false`. Treść odpowiedzi credential/identity nie jest publikowana.
macOS/Linux sprzątają grupę procesów kontroli; odłączające się procesy nie są
sandboxowane. Wbudowane checks to odczyty: STS GetCallerIdentity, EKS
DescribeCluster i lokalny kubectl config view. Credential command autora
manifestu jest zaufanym programem — używaj tu tylko read-only poleceń.

## Migracja legacy jest celowym bezpiecznym zatrzymaniem

Konfiguracja bez target nadal daje się wczytać i renderować. Nonstrict preflight
pokazuje brakujące assertions i `ok: false`. Strict oraz apply/destroy workflow
infrastrukturalnych odmawiają z `PLATFORMCTL_TARGET_REQUIRED`, dopóki operator
nie zadeklaruje oczekiwanej tożsamości i nie zatwierdzi nowego planu. Dotyczy to
AWS/Terraform, kubectl/helm/kind i jawnych requires_cluster. Lokalny workflow
echo nie wymaga sztucznego target. To świadoma zmiana: wcześniejszy sukces
kontroli zależności nie potwierdzał konta ani klastra. Nie przepisujemy state
ani opublikowanych szablonów. Obsługiwane assertions to AWS i Kubernetes;
inny provider Terraform nie dostaje fikcyjnego potwierdzenia tożsamości AWS.

```yaml
target:
  cluster_name: demo-platform # odpowiada values.cluster_name
  aws:
    account: "123456789012"
    region: eu-central-1
    profile: develop
  kubernetes:
    kubeconfig: .platformctl/targets/demo.kubeconfig
    context: arn:aws:eks:eu-central-1:123456789012:cluster/demo-platform
    cluster: arn:aws:eks:eu-central-1:123456789012:cluster/demo-platform
    endpoint_source: eks
```

AWS wymaga account/region/profile. STS i rzeczywiste procesy workflow dostają
ten sam profil i region. Odziedziczone static/web-identity credentials są dla
nich wyłączone; profil musi działać bez nich. Zadeklarowane wartości
region/aws_profile muszą być zgodne; Terraform wymaga obu. Sprzeczne selektory,
w tym EKS --name/--cluster-name, są odrzucane przed generowaniem. Target nie
może być zastąpiony przez AWS_ENDPOINT_URL*, --endpoint-url, --no-sign-request,
TF_CLI_ARGS*, -chdir, -var ani -var-file.

HOME, KUBECONFIG, AWS_CONFIG_FILE, AWS_SHARED_CREDENTIALS_FILE i AWS_CA_BUNDLE
są rozwiązywane do tych samych absolutnych ścieżek przed zmianą cwd dziecka.
Strict preflight sprawdza także katalogi kroków przed generowaniem; jeśli
wymagany katalog jeszcze nie istnieje, najpierw uruchom plan i przejrzyj
renderowane pliki. Checker sam tych katalogów nie tworzy. STS przed krokiem
działa też w jego rzeczywistym cwd, aby względne odwołanie
wewnątrz profilu nie wybrało innego konta niż kontrola w katalogu głównym.
Rodzic nie zmienia swojego środowiska. Autor musi honorować te same selektory
w providerach/backendach HCL i skryptach. CLI nie analizuje ich pełnej semantyki
ani nie izoluje dowolnych programów — to trusted-template boundary.

Kubernetes porównuje context, cluster i HTTPS endpoint. Tryb statyczny wymaga
server. `endpoint_source: eks` dopuszcza brak server: bounded DescribeCluster
musi zwrócić ACTIVE i zgodne name/ARN/account/region. Endpoint i zdekodowane
inline CA muszą odpowiadać rzeczywistemu scoped kubeconfig. Nie publikujemy
jego user credentiali. To nie jest dowód readiness, zdalnego UID ani deploy.

## Jedno jawne przejście bootstrapu

`target.bootstrap: true` należy do zatwierdzanego digestu. Wymaga cluster_name,
endpoint_source eks/kind i jednego prywatnego pliku
`.platformctl/targets/*.kubeconfig`, który początkowo nie istnieje. CLI nie
przejmuje zastanego pliku, symlinku, hardlinku ani globalnego kubeconfig.
Katalog targets jest własnością CLI i ma prawa 0700; nowy plik musi być zwykłym
plikiem właściciela, bez praw grupy/innych, z jednym linkiem, do 128 KiB.
Bootstrap CA jest inline. Pozostałe target configs są ograniczone do 1 MiB.

Apply musi mieć dokładnie jeden marker przed operacjami na klastrze:

```yaml
steps:
  apply:
    # wcześniejszy terraform apply tworzy EKS na zweryfikowanym koncie
    - name: configure private EKS target
      command: aws
      args: [eks, update-kubeconfig, --name, "{{ .Values.cluster_name }}",
             --region, "{{ .Values.region }}", --profile, "{{ .Values.aws_profile }}"]
      establishes_cluster_target: true
```

KUBECONFIG wskazuje prywatny plik. Argument --kubeconfig nie może przekierować
zapisu. Marker nie ma retry >1 i nie wymaga istniejącego klastra. Własne skrypty
wykonujące późniejsze operacje na klastrze muszą mieć `requires_cluster: true`.
Bezpośrednie kubectl i helm poza repo/version są rozpoznawane automatycznie.
Kontrola jest ponawiana przed każdą próbą kroku.

Przed markerem state zapisuje pending. Po sukcesie marker musi utworzyć plik
i przejść pełną kontrolę identity. Wszystkie źródła, wartości, kroki,
wygenerowane bajty i pozostałe konfiguracje muszą pozostać identyczne z
pierwotnym podglądem. Dozwolony jest wyłącznie drift missing → obserwowane bytes
tego jednego pliku. Potem zwykły snapshot wiąże także jego nowy hash.

Metadata zawiera wersjonowany receipt: origin_approved_hash,
policy_accepted_hash, observed_hash, path i marker. Origin jest weryfikowany
przez rekonstrukcję pierwotnego missing-file digestu. Receipt, nowy digest i
completed checkpoint są zapisywane jednym atomowym state.Save. To jedna
pierwotnie zatwierdzona policy transition, nie nowe zatwierdzenie człowieka.

Crash przed receipt, błąd jego zapisu, brak receipt albo późniejszy drift
powoduje odmowę resume. CLI nie naprawia state ani nie przejmuje pozostałego
pliku. Zachowaj dowody i zasoby; po inspekcji użyj istniejącego target/workflow
z bootstrap false i nowym planem albo świeżego workspace. Nie usuwaj realnych
zasobów ani starego state automatycznie. Destroy przy bootstrap policy wymaga
poprawnego receipt. Dotychczasowy state bez receipt pozostaje czytelny,
jednak nie otrzymuje automatycznie brakującej zgody. Metadata używa istniejącej
mapy, bez usuwania innych kluczy. Stary CLI odrzuci target/marker strict parserem.

Dla Kind wymagamy `endpoint_source: kind`, context/cluster
`kind-<cluster_name>` i predeclared HTTPS loopback IP z portem w server.
Manifest musi ustawić ten sam API server port i oznaczyć create markerem.
Stary stock workflow z dynamicznym portem nie ma pełnej zgodności bootstrapu;
paired aktualizacja szablonu jest osobnym etapem. Nie deklarujemy jej odbioru.

Po bootstrapie dalszy istniejący workflow nie powinien powtarzać creation
marker. Zmiana polityki wymaga replan. Rollback CLI wymaga idle workspace i
świadomej migracji konfiguracji/nowego zatwierdzenia; cofnięcie binarki nie cofa
infrastruktury i nie zmienia kubeconfig/state automatycznie.
