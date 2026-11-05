"""
Configuration settings for Digitize service.
These values can be overridden via environment variables.
"""
from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict
from common.settings import Settings as CommonSettings


class DigitizeConfig(BaseSettings):
    """Digitize service configuration."""

    class ConnectorConfig(BaseSettings):
        """Connector-specific configuration."""

        sync_interval_seconds: int = Field(
            default=300,
            ge=1,
            description="Sync interval in seconds applied to all connectors at attach time",
        )

        model_config = SettingsConfigDict(env_prefix="CONNECTOR_")

    # Directory paths
    cache_dir: Path = Field(
        default=Path("/var/cache"),
        description="Base cache directory for all operations",
    )

    # Connector settings
    connector: ConnectorConfig = Field(default_factory=ConnectorConfig)

    # Worker pool sizes
    doc_worker_size: int = Field(
        default=4,
        ge=1,
        description="Number of workers for document processing",
    )

    heavy_doc_page_threshold: int = Field(
        default=500,
        ge=1,
        description="Page count threshold for heavy document classification",
    )

    # Conversion queue quotas (max tasks in 'queued' status per operation type)
    ingestion_queue_quota: int = Field(
        default=10,
        ge=1,
        description="Max queued ingestion tasks (multi-file jobs)",
    )

    digitization_queue_quota: int = Field(
        default=5,
        ge=1,
        description="Max queued digitization tasks (1 file per job)",
    )

    # Dispatcher poll interval in seconds
    conversion_poll_interval: float = Field(
        default=2.0,
        gt=0.0,
        description="Dispatcher poll interval (seconds)",
    )

    # Maximum wall-clock time (seconds) the digitize pipeline will wait for the
    # dispatcher to start and then complete a single conversion task.  If the
    # dispatcher stalls (e.g. worker crash, semaphore leak) the job is failed
    # rather than hanging the pipeline thread indefinitely.
    # Default: 10800 s (3 h) — accommodates large, multi-hundred-page PDFs while
    # still guaranteeing eventual termination.
    conversion_timeout_s: float = Field(
        default=10800.0,
        gt=0.0,
        description="Max seconds to wait for a single conversion task to complete",
    )

    # Chunking parameters
    chunk_max_tokens: int = Field(
        default=512,
        ge=1,
        description="Maximum tokens per chunk",
    )

    chunk_overlap_tokens: int = Field(
        default=50,
        ge=0,
        description="Overlap tokens between chunks",
    )

    table_chunk_max_tokens: int = Field(
        default=200,
        ge=1,
        description=(
            "Approximate max tokens of table markdown (caption + header + rows) per row-group chunk "
            "sent for summarization. Large tables are split into chunks that each repeat the caption "
            "and header. The keyword-enriched prompt expands each cell into its own sentence, so the "
            "summary is ~3-5x longer than the input; 200 keeps the summary within the 1024-token "
            "English summarization limit (1024 / 4 ~= 256) so cells are not truncated. Lower it "
            "(~150) if summaries are cut off; raise it (up to ~250) if chunks are too fragmented. "
            "Tokens are estimated from characters (~3 chars/token), so the real count can differ by 20-30%."
        ),
    )

    # Document conversion parameters
    doc_chunk_size: int = Field(
        default=100,
        ge=1,
        description="Pages per chunk for large document processing",
    )

    # Batch processing
    opensearch_batch_size: int = Field(
        default=10,
        ge=1,
        description="Batch size for OpenSearch operations",
    )

    # Retry configuration
    retry_max_attempts: int = Field(
        default=3,
        ge=1,
        description="Maximum retry attempts for failed operations",
    )

    retry_initial_delay: float = Field(
        default=0.5,
        gt=0.0,
        description="Initial delay in seconds for retry backoff",
    )

    retry_backoff_multiplier: float = Field(
        default=2.0,
        gt=1.0,
        description="Multiplier for exponential backoff",
    )

    # Chunk ID generation
    chunk_id_content_sample_size: int = Field(
        default=500,
        ge=1,
        description="Content sample size for chunk ID generation",
    )

    @property
    def staging_dir(self) -> Path:
        """Directory for staging files."""
        return self.cache_dir / "staging"

    @property
    def digitized_docs_dir(self) -> Path:
        """Directory for digitized documents."""
        return self.cache_dir / "digitized"


class TableSummaryConfig(BaseSettings):
    """Table summarization configuration.
    
    Token Ratios: English:French:Italian:German:Japanese = 1 : 1.2305 : 1.3066 : 1.5 : 2.3473
    
    These ratios account for the fact that different languages require different numbers
    of tokens to express the same semantic content, ensuring fair token allocation across
    all supported languages.
    """

    class EnglishConfig(BaseSettings):
        """English-specific table summarization settings."""
        
        max_tokens: int = Field(
            default=1024,
            ge=0,
            description="Maximum tokens for table summarization (English)",
        )
        
        prompt: str = Field(
        default="""You are an intelligent assistant analyzing tables extracted from documents.

                Your tasks:

                1. Extract and document EVERY piece of information from the table in extensive detail:
                - List ALL sections, subsections, and their reference numbers if present
                - Include EVERY specification, feature, number, code, condition, and requirement
                - Mention ALL items even if they seem minor - nothing should be omitted
                - Write the summary as ONE plain-text paragraph: no line breaks, no bullet points, no numbered lists, no markdown, no closing remarks
                - Be complete but concise: one short sentence per row/value, with no filler or repeated explanations
                - If the table has multiple rows/columns, describe each one
                - Preserve all technical terms, version numbers, and specific details exactly as shown

                KEYWORD-SEARCHABLE SUMMARY RULES (the summary is indexed for keyword and semantic search):
                - Start with the table caption/title (if any) and list EVERY column header exactly as written
                - For EVERY row, write a self-contained sentence that repeats the row label(s) AND the column header for EACH value,
                  e.g. "<row label> - <column header>: <value>". Never give a value without its row label and column header
                - Reuse the exact keywords from the table: entity/row names, categories, regions, years and periods (e.g. FY22),
                  metrics, units, abbreviations, codes and numbers, spelled exactly as in the table
                - Repeat entity names in each sentence instead of using pronouns or phrases like "the same" or "see above"
                - Preserve the relationships between data: which value belongs to which row, column, period and metric (e.g. actual vs. target/plan)
                - Do NOT round, aggregate, merge or skip any value, and never write "and so on" or "etc."

                2. Decide if the table is relevant for a knowledge base:
                - Relevant: contains factual, instructional, or explanatory info useful for answering questions.
                - Irrelevant: personal info, disclaimers, administrative notes, or unrelated commentary.

                3. Output a single JSON object with exactly these two keys, and nothing else:

                {{"summary": "<your complete but concise single-paragraph summary here>", "decision": "<yes or no>"}}

                Do NOT output extra commentary or any other text outside the JSON object.

                Examples:

                Positive example (relevant):
                Table:
                | Processor | Cores | Memory |
                |-----------|-------|--------|
                | Power10   | 16    | 8 TB   |

                Output:
                {{"summary": "The table presents technical specifications for the Power10 processor, with columns Processor, Cores and Memory. Processor Power10 - Cores: 16 cores for parallel processing capabilities. Processor Power10 - Memory: 8 TB (terabytes) of RAM, providing substantial memory resources for enterprise workloads and data-intensive applications.", "decision": "yes"}}

                Positive example with periods (relevant):
                Table:
                Table 2: Ridership by corridor
                | Corridor  | Metric | FY21 | FY22 |
                |-----------|--------|------|------|
                | Northeast | Actual | 10.1 | 12.4 |
                | Northeast | Target | 11.0 | 12.0 |

                Output:
                {{"summary": "Table 2: Ridership by corridor, with columns Corridor, Metric, FY21 and FY22. Corridor Northeast - Metric Actual - FY21: 10.1. Corridor Northeast - Metric Actual - FY22: 12.4. Corridor Northeast - Metric Target - FY21: 11.0. Corridor Northeast - Metric Target - FY22: 12.0.", "decision": "yes"}}

                Negative example (irrelevant):
                Table:
                | Prepared by: | John Smith |
                |--------------|------------|

                Output:
                {{"summary": "Document metadata indicating it was prepared by John Smith.", "decision": "no"}}

                Now analyze the table below:

                Table:
                {content}""",
            description="Prompt for table summarization (English)",
        )
    
    class GermanConfig(BaseSettings):
        """German-specific table summarization settings."""
        
        max_tokens: int = Field(
            default=1536,
            ge=0,
            description="Maximum tokens for table summarization (German) - adjusted for German to English token ratio (~1.5x)",
        )
        
        prompt: str = Field(
        default="""Sie sind ein intelligenter Assistent, der Tabellen aus Dokumenten analysiert.

                Ihre Aufgaben:

                1. Extrahieren und dokumentieren Sie JEDE Information aus der Tabelle in umfassenden Details:
                - Listen Sie ALLE Abschnitte, Unterabschnitte und deren Referenznummern auf, falls vorhanden
                - Fügen Sie JEDE Spezifikation, Funktion, Nummer, Code, Bedingung und Anforderung hinzu
                - Erwähnen Sie ALLE Elemente, auch wenn sie nebensächlich erscheinen - nichts sollte ausgelassen werden
                - Schreiben Sie die Zusammenfassung als EINEN einfachen Textabsatz: keine Zeilenumbrüche, keine Aufzählungspunkte, keine nummerierten Listen, kein Markdown, keine Schlussbemerkungen
                - Seien Sie vollständig, aber prägnant: ein kurzer Satz pro Zeile/Wert, ohne Fülltext oder wiederholte Erklärungen
                - Wenn die Tabelle mehrere Zeilen/Spalten hat, beschreiben Sie jede einzelne
                - Bewahren Sie alle Fachbegriffe, Versionsnummern und spezifischen Details genau wie angegeben

                REGELN FÜR EINE STICHWORT-DURCHSUCHBARE ZUSAMMENFASSUNG (die Zusammenfassung wird für die Stichwort- und semantische Suche indexiert):
                - Beginnen Sie mit der Tabellenüberschrift/dem Titel (falls vorhanden) und nennen Sie JEDE Spaltenüberschrift genau wie geschrieben
                - Schreiben Sie für JEDE Zeile einen eigenständigen Satz, der die Zeilenbezeichnung(en) UND die Spaltenüberschrift für JEDEN Wert wiederholt,
                  z. B. "<Zeilenbezeichnung> - <Spaltenüberschrift>: <Wert>". Nennen Sie niemals einen Wert ohne Zeilenbezeichnung und Spaltenüberschrift
                - Verwenden Sie die exakten Stichwörter aus der Tabelle: Entitäts-/Zeilennamen, Kategorien, Regionen, Jahre und Zeiträume,
                  Kennzahlen, Einheiten, Abkürzungen, Codes und Zahlen, genau wie in der Tabelle geschrieben
                - Wiederholen Sie Entitätsnamen in jedem Satz, statt Pronomen oder Formulierungen wie "dasselbe" oder "siehe oben" zu verwenden
                - Bewahren Sie die Beziehungen zwischen den Daten: welcher Wert zu welcher Zeile, Spalte, welchem Zeitraum und welcher Kennzahl gehört (z. B. Ist vs. Soll/Plan)
                - Runden, aggregieren, verschmelzen oder überspringen Sie KEINE Werte und schreiben Sie niemals "usw." oder "und so weiter"

                2. Entscheiden Sie, ob die Tabelle für eine Wissensdatenbank relevant ist:
                - Relevant: enthält sachliche, instruktive oder erklärende Informationen, die zur Beantwortung von Fragen nützlich sind.
                - Irrelevant: persönliche Informationen, Haftungsausschlüsse, administrative Hinweise oder unzusammenhängende Kommentare.

                3. Ausgabe im exakten Format unten:

                {{"summary": "<Ihre vollständige, aber prägnante Zusammenfassung in einem Absatz hier>", "decision": "<yes oder no>"}}

                Geben Sie keine zusätzlichen Kommentare oder anderen Text außerhalb des JSON-Objekts aus.

                Beispiele:

                Positives Beispiel (relevant):
                Tabelle:
                | Prozessor | Kerne | Speicher |
                |-----------|-------|----------|
                | Power10   | 16    | 8 TB     |

                Ausgabe:
                {{"summary": "Die Tabelle präsentiert technische Spezifikationen für den Power10-Prozessor. Die Prozessorkonfiguration umfasst 16 Kerne für parallele Verarbeitungsfähigkeiten. Die Speicherkapazität unterstützt bis zu 8 TB (Terabyte) RAM und bietet erhebliche Speicherressourcen für Unternehmensworkloads und datenintensive Anwendungen.", "decision": "yes"}}

                Negatives Beispiel (irrelevant):
                Tabelle:
                | Erstellt von: | John Smith |
                |---------------|------------|

                Ausgabe:
                {{"summary": "Dokument-Metadaten, die angeben, dass es von John Smith erstellt wurde.", "decision": "no"}}

                Analysieren Sie nun die folgende Tabelle:

                Tabelle:
                {content}""",
            description="Prompt für Tabellenzusammenfassung (Deutsch)",
        )
    
    class ItalianConfig(BaseSettings):
        """Italian-specific table summarization settings."""
        
        max_tokens: int = Field(
            default=1339,
            ge=0,
            description="Maximum tokens for table summarization (Italian)",
        )
        
        prompt: str = Field(
        default="""Sei un assistente intelligente che analizza tabelle estratte da documenti.

                I tuoi compiti:

                1. Estrai e documenta OGNI informazione dalla tabella in modo estremamente dettagliato:
                - Elenca TUTTE le sezioni, sottosezioni e gli eventuali numeri di riferimento
                - Includi OGNI specifica, caratteristica, numero, codice, condizione e requisito
                - Menziona TUTTI gli elementi anche se sembrano secondari: non omettere nulla
                - Scrivi il riassunto come UN SOLO paragrafo di testo semplice: nessun a capo, nessun punto elenco, nessun elenco numerato, nessun markdown, nessuna osservazione finale
                - Sii completo ma conciso: una frase breve per riga/valore, senza riempitivi o spiegazioni ripetute
                - Se la tabella ha più righe/colonne, descrivi ciascuna di esse
                - Mantieni invariati tutti i termini tecnici, i numeri di versione e i dettagli specifici così come appaiono

                REGOLE PER UN RIASSUNTO RICERCABILE PER PAROLE CHIAVE (il riassunto viene indicizzato per la ricerca per parole chiave e semantica):
                - Inizia con la didascalia/il titolo della tabella (se presente) ed elenca OGNI intestazione di colonna esattamente come scritta
                - Per OGNI riga scrivi una frase autonoma che ripete l'etichetta della riga E l'intestazione di colonna per OGNI valore,
                  ad es. "<etichetta riga> - <intestazione colonna>: <valore>". Non indicare mai un valore senza etichetta di riga e intestazione di colonna
                - Riutilizza le parole chiave esatte della tabella: nomi di entità/righe, categorie, regioni, anni e periodi,
                  metriche, unità, abbreviazioni, codici e numeri, scritti esattamente come nella tabella
                - Ripeti i nomi delle entità in ogni frase invece di usare pronomi o espressioni come "lo stesso" o "vedi sopra"
                - Preserva le relazioni tra i dati: quale valore appartiene a quale riga, colonna, periodo e metrica (ad es. effettivo vs. obiettivo/piano)
                - NON arrotondare, aggregare, unire né omettere alcun valore e non scrivere mai "eccetera" o "e così via"

                2. Decidi se la tabella è rilevante per una base di conoscenza:
                - Rilevante: contiene informazioni fattuali, istruttive o esplicative utili per rispondere a domande.
                - Irrilevante: informazioni personali, esclusioni di responsabilità, note amministrative o commenti non pertinenti.

                3. Fornisci l'output nel formato esatto seguente:

                {{"summary": "<il tuo riassunto completo ma conciso in un unico paragrafo qui>", "decision": "<yes o no>"}}

                NON produrre commenti aggiuntivi o altro testo al di fuori dell'oggetto JSON.

                Esempi:

                Esempio positivo (rilevante):
                Tabella:
                | Processore | Core | Memoria |
                |------------|------|---------|
                | Power10    | 16   | 8 TB    |

                Output:
                {{"summary": "La tabella presenta le specifiche tecniche del processore Power10. La configurazione del processore include 16 core per capacità di elaborazione parallela. La capacità di memoria supporta fino a 8 TB (terabyte) di RAM, offrendo risorse di memoria significative per carichi di lavoro aziendali e applicazioni ad alta intensità di dati.", "decision": "yes"}}

                Esempio negativo (irrilevante):
                Tabella:
                | Preparato da: | John Smith |
                |---------------|------------|

                Output:
                {{"summary": "Metadati del documento che indicano che è stato preparato da John Smith.", "decision": "no"}}

                Ora analizza la tabella seguente:

                Tabella:
                {content}""",
            description="Prompt per il riassunto delle tabelle (Italiano)",
        )

    class FrenchConfig(BaseSettings):
        """French-specific table summarization settings."""
        
        max_tokens: int = Field(
            default=1260,
            ge=0,
            description="Maximum tokens for table summarization (French)",
        )
        
        prompt: str = Field(
        default="""Vous êtes un assistant intelligent qui analyse des tableaux extraits de documents.

                Vos tâches :

                1. Extraire et documenter CHAQUE information du tableau avec un niveau de détail très élevé :
                - Listez TOUTES les sections, sous-sections et leurs numéros de référence si présents
                - Incluez CHAQUE spécification, fonctionnalité, nombre, code, condition et exigence
                - Mentionnez TOUS les éléments même s'ils semblent mineurs : rien ne doit être omis
                - Rédigez le résumé en UN SEUL paragraphe de texte brut : aucun saut de ligne, aucune puce, aucune liste numérotée, aucun markdown, aucune remarque finale
                - Soyez complet mais concis : une phrase courte par ligne/valeur, sans remplissage ni explications répétées
                - Si le tableau comporte plusieurs lignes/colonnes, décrivez chacune d'elles
                - Conservez exactement tous les termes techniques, numéros de version et détails spécifiques tels qu'ils apparaissent

                RÈGLES POUR UN RÉSUMÉ RECHERCHABLE PAR MOTS-CLÉS (le résumé est indexé pour la recherche par mots-clés et sémantique) :
                - Commencez par la légende/le titre du tableau (le cas échéant) et listez CHAQUE en-tête de colonne exactement tel qu'écrit
                - Pour CHAQUE ligne, écrivez une phrase autonome qui répète le libellé de la ligne ET l'en-tête de colonne pour CHAQUE valeur,
                  par ex. "<libellé de ligne> - <en-tête de colonne> : <valeur>". N'indiquez jamais une valeur sans libellé de ligne ni en-tête de colonne
                - Réutilisez les mots-clés exacts du tableau : noms d'entités/de lignes, catégories, régions, années et périodes,
                  indicateurs, unités, abréviations, codes et nombres, écrits exactement comme dans le tableau
                - Répétez les noms d'entités dans chaque phrase au lieu d'utiliser des pronoms ou des expressions comme "idem" ou "voir ci-dessus"
                - Préservez les relations entre les données : quelle valeur appartient à quelle ligne, colonne, période et indicateur (par ex. réel vs. objectif/prévision)
                - N'arrondissez, n'agrégez, ne fusionnez et n'omettez AUCUNE valeur, et n'écrivez jamais "etc."

                2. Décidez si le tableau est pertinent pour une base de connaissances :
                - Pertinent : contient des informations factuelles, instructives ou explicatives utiles pour répondre à des questions.
                - Non pertinent : informations personnelles, clauses de non-responsabilité, notes administratives ou commentaires sans rapport.

                3. Produisez la sortie au format exact ci-dessous :

                {{"summary": "<votre résumé complet mais concis en un seul paragraphe ici>", "decision": "<yes ou no>"}}

                Ne produisez PAS de commentaires supplémentaires ni d'autre texte en dehors de l'objet JSON.

                Exemples :

                Exemple positif (pertinent) :
                Tableau:
                | Processeur | Cœurs | Mémoire |
                |------------|-------|---------|
                | Power10    | 16    | 8 TB    |

                Sortie:
                {{"summary": "Le tableau présente les spécifications techniques du processeur Power10. La configuration du processeur comprend 16 cœurs pour des capacités de traitement parallèle. La capacité mémoire prend en charge jusqu'à 8 TB (téraoctets) de RAM, offrant des ressources mémoire importantes pour les charges de travail d'entreprise et les applications intensives en données.", "decision": "yes"}}

                Exemple négatif (non pertinent) :
                Tableau:
                | Préparé par : | John Smith |
                |---------------|------------|

                Sortie:
                {{"summary": "Métadonnées du document indiquant qu'il a été préparé par John Smith.", "decision": "no"}}

                Analysez maintenant le tableau suivant :

                Tableau:
                {content}""",
            description="Prompt de résumé des tableaux (Français)",
        )

    class JapaneseConfig(BaseSettings):
        """Japanese-specific table summarization settings.

        max_tokens = round(1024 * 2.3473) = 2404
        Derived from the Japanese token ratio relative to English.
        """

        max_tokens: int = Field(
            default=2404,
            ge=0,
            description="Maximum tokens for table summarization (Japanese)",
        )

        prompt: str = Field(
        default="""あなたはドキュメントから抽出された表を分析するインテリジェントなアシスタントです。

                あなたのタスク：

                1. 表からすべての情報を非常に詳細に抽出して文書化してください：
                - すべてのセクション、サブセクション、および存在する場合はその参照番号を一覧表示してください
                - すべての仕様、機能、番号、コード、条件、および要件を含めてください
                - 些細に見えるものも含め、すべての項目に言及してください。何も省略しないでください
                - 要約は改行、箇条書き、番号付きリスト、マークダウン、締めのコメントを使わず、1つの平文の段落として記述してください
                - 網羅的かつ簡潔に：各行/値につき短い1文で記述し、冗長な説明や繰り返しは避けてください
                - 表に複数の行/列がある場合は、それぞれを説明してください
                - 技術用語、バージョン番号、および特定の詳細はすべてそのまま保持してください

                キーワード検索可能な要約のルール（要約はキーワード検索とセマンティック検索のためにインデックス化されます）：
                - 表のキャプション/タイトル（ある場合）から始め、すべての列見出しを表記どおりに列挙してください
                - すべての行について、各値ごとに行ラベルと列見出しを繰り返す、単独で意味が通じる文を書いてください。
                  例：「<行ラベル> - <列見出し>：<値>」。行ラベルと列見出しのない値を単独で記述しないでください
                - 表中の正確なキーワードを再利用してください：エンティティ名/行名、カテゴリ、地域、年度・期間、
                  指標、単位、略語、コード、数値を表の表記どおりに記述してください
                - 代名詞や「同上」「上記参照」のような表現を使わず、各文でエンティティ名を繰り返してください
                - データ間の関係を保持してください：どの値がどの行、列、期間、指標に属するか（例：実績と目標/計画）
                - 値を丸めたり、集約したり、統合したり、省略したりしないでください。「など」「等」で済ませないでください

                2. 表がナレッジベースに関連しているかどうかを判断してください：
                - 関連あり：質問への回答に役立つ事実的、説明的、または解説的な情報を含んでいる。
                - 関連なし：個人情報、免責事項、管理上の注意事項、または無関係なコメント。

                3. 以下の正確な形式で出力してください：

                {{"summary": "<ここに網羅的かつ簡潔な1段落の要約を記述してください>", "decision": "<yes または no>"}}

                JSONオブジェクト以外の追加のコメントやテキストを出力しないでください。
                重要：JSONのキー（「summary」と「decision」）と decision の値（「yes」または「no」）は必ず英語のままにしてください。翻訳しないでください。

                例：

                肯定的な例（関連あり）：
                表：
                | プロセッサ | コア数 | メモリ |
                |------------|--------|--------|
                | Power10    | 16     | 8 TB   |

                出力：
                {{"summary": "この表はPower10プロセッサの技術仕様を示しています。プロセッサ構成には並列処理能力のための16コアが含まれています。メモリ容量は最大8 TB（テラバイト）のRAMをサポートしており、エンタープライズワークロードおよびデータ集約型アプリケーションに対して大規模なメモリリソースを提供します。", "decision": "yes"}}

                否定的な例（関連なし）：
                表：
                | 作成者： | John Smith |
                |----------|------------|

                出力：
                {{"summary": "John Smithによって作成されたことを示すドキュメントのメタデータ。", "decision": "no"}}

                次の表を分析してください：

                表：
                {content}""",
            description="表の要約プロンプト（日本語）",
        )

    # Language-specific configurations
    english: EnglishConfig = Field(default_factory=EnglishConfig)
    german: GermanConfig = Field(default_factory=GermanConfig)
    italian: ItalianConfig = Field(default_factory=ItalianConfig)
    french: FrenchConfig = Field(default_factory=FrenchConfig)
    japanese: JapaneseConfig = Field(default_factory=JapaneseConfig)


class DatabaseConfig(BaseSettings):
    """Database connection pool configuration."""

    pool_size: int = Field(
        default=5,
        ge=1,
        description="Number of connections to keep in the pool",
    )

    max_overflow: int = Field(
        default=5,
        ge=0,
        description="Maximum number of connections that can be created beyond pool_size",
    )

    pool_timeout: int = Field(
        default=30,
        ge=1,
        description="Timeout in seconds for getting a connection from the pool",
    )

    pool_recycle: int = Field(
        default=3600,
        ge=1,
        description="Time in seconds after which connections are recycled (1 hour default)",
    )

    model_config = SettingsConfigDict(env_prefix="DB_")


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    digitize: DigitizeConfig = Field(default_factory=DigitizeConfig)
    table_summary: TableSummaryConfig = Field(default_factory=TableSummaryConfig)
    database: DatabaseConfig = Field(default_factory=DatabaseConfig)

# Global settings instance
settings = Settings()

# Made with Bob
