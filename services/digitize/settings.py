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

    # Directory paths
    cache_dir: Path = Field(
        default=Path("/var/cache"),
        description="Base cache directory for all operations",
    )

    # Worker pool sizes
    doc_worker_size: int = Field(
        default=4,
        ge=1,
        description="Number of workers for document processing",
    )

    heavy_doc_convert_worker_size: int = Field(
        default=2,
        ge=1,
        description="Number of workers for heavy document conversion",
    )

    heavy_doc_page_threshold: int = Field(
        default=500,
        ge=1,
        description="Page count threshold for heavy document classification",
    )

    # API concurrency limits
    digitization_concurrency_limit: int = Field(
        default=2,
        ge=1,
        description="Concurrency limit for digitization API",
    )

    ingestion_concurrency_limit: int = Field(
        default=1,
        ge=1,
        description="Concurrency limit for ingestion API",
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
    
    Token Ratios: English:French:Italian:German = 1 : 1.2305 : 1.3066 : 1.5
    
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
                - Use structured format with clear organization (numbered lists, bullet points, or detailed paragraphs)
                - Be extremely thorough and comprehensive - aim for maximum detail
                - If the table has multiple rows/columns, describe each one
                - Preserve all technical terms, version numbers, and specific details exactly as shown

                2. Decide if the table is relevant for a knowledge base:
                - Relevant: contains factual, instructional, or explanatory info useful for answering questions.
                - Irrelevant: personal info, disclaimers, administrative notes, or unrelated commentary.

                3. Output in the exact format below:

                Summary: <your extremely detailed summary here - be verbose and comprehensive>
                Decision: <yes or no>

                Do NOT output JSON, extra commentary, or any other text.

                Examples:

                Positive example (relevant):
                Table:
                | Processor | Cores | Memory |
                |-----------|-------|--------|
                | Power10   | 16    | 8 TB   |

                Output:
                Summary: The table presents technical specifications for the Power10 processor. The processor configuration includes 16 cores for parallel processing capabilities. The memory capacity supports up to 8 TB (terabytes) of RAM, providing substantial memory resources for enterprise workloads and data-intensive applications.
                Decision: yes

                Negative example (irrelevant):
                Table:
                | Prepared by: | John Smith |
                |--------------|------------|

                Output:
                Summary: Document metadata indicating it was prepared by John Smith.
                Decision: no

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
                - Verwenden Sie ein strukturiertes Format mit klarer Organisation (nummerierte Listen, Aufzählungspunkte oder detaillierte Absätze)
                - Seien Sie äußerst gründlich und umfassend - streben Sie maximale Detailtiefe an
                - Wenn die Tabelle mehrere Zeilen/Spalten hat, beschreiben Sie jede einzelne
                - Bewahren Sie alle Fachbegriffe, Versionsnummern und spezifischen Details genau wie angegeben

                2. Entscheiden Sie, ob die Tabelle für eine Wissensdatenbank relevant ist:
                - Relevant: enthält sachliche, instruktive oder erklärende Informationen, die zur Beantwortung von Fragen nützlich sind.
                - Irrelevant: persönliche Informationen, Haftungsausschlüsse, administrative Hinweise oder unzusammenhängende Kommentare.

                3. Ausgabe im exakten Format unten:

                Summary: <Ihre äußerst detaillierte Zusammenfassung hier - seien Sie ausführlich und umfassend>
                Decision: <yes oder no>

                Geben Sie KEIN JSON, zusätzliche Kommentare oder anderen Text aus.

                Beispiele:

                Positives Beispiel (relevant):
                Tabelle:
                | Prozessor | Kerne | Speicher |
                |-----------|-------|----------|
                | Power10   | 16    | 8 TB     |

                Ausgabe:
                Summary: Die Tabelle präsentiert technische Spezifikationen für den Power10-Prozessor. Die Prozessorkonfiguration umfasst 16 Kerne für parallele Verarbeitungsfähigkeiten. Die Speicherkapazität unterstützt bis zu 8 TB (Terabyte) RAM und bietet erhebliche Speicherressourcen für Unternehmensworkloads und datenintensive Anwendungen.
                Decision: yes

                Negatives Beispiel (irrelevant):
                Tabelle:
                | Erstellt von: | John Smith |
                |---------------|------------|

                Ausgabe:
                Summary: Dokument-Metadaten, die angeben, dass es von John Smith erstellt wurde.
                Decision: no

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
                - Usa un formato strutturato con organizzazione chiara (elenchi numerati, punti elenco o paragrafi dettagliati)
                - Sii estremamente accurato e completo: punta al massimo livello di dettaglio
                - Se la tabella ha più righe/colonne, descrivi ciascuna di esse
                - Mantieni invariati tutti i termini tecnici, i numeri di versione e i dettagli specifici così come appaiono

                2. Decidi se la tabella è rilevante per una base di conoscenza:
                - Rilevante: contiene informazioni fattuali, istruttive o esplicative utili per rispondere a domande.
                - Irrilevante: informazioni personali, esclusioni di responsabilità, note amministrative o commenti non pertinenti.

                3. Fornisci l'output nel formato esatto seguente:

                Summary: <il tuo riassunto estremamente dettagliato qui - sii completo e approfondito>
                Decision: <yes o no>

                NON produrre JSON, commenti aggiuntivi o altro testo.

                Esempi:

                Esempio positivo (rilevante):
                Tabella:
                | Processore | Core | Memoria |
                |------------|------|---------|
                | Power10    | 16   | 8 TB    |

                Output:
                Summary: La tabella presenta le specifiche tecniche del processore Power10. La configurazione del processore include 16 core per capacità di elaborazione parallela. La capacità di memoria supporta fino a 8 TB (terabyte) di RAM, offrendo risorse di memoria significative per carichi di lavoro aziendali e applicazioni ad alta intensità di dati.
                Decision: yes

                Esempio negativo (irrilevante):
                Tabella:
                | Preparato da: | John Smith |
                |---------------|------------|

                Output:
                Summary: Metadati del documento che indicano che è stato preparato da John Smith.
                Decision: no

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
                - Utilisez un format structuré avec une organisation claire (listes numérotées, puces ou paragraphes détaillés)
                - Soyez extrêmement minutieux et exhaustif : visez un niveau de détail maximal
                - Si le tableau comporte plusieurs lignes/colonnes, décrivez chacune d'elles
                - Conservez exactement tous les termes techniques, numéros de version et détails spécifiques tels qu'ils apparaissent

                2. Décidez si le tableau est pertinent pour une base de connaissances :
                - Pertinent : contient des informations factuelles, instructives ou explicatives utiles pour répondre à des questions.
                - Non pertinent : informations personnelles, clauses de non-responsabilité, notes administratives ou commentaires sans rapport.

                3. Produisez la sortie au format exact ci-dessous :

                Summary: <votre résumé extrêmement détaillé ici - soyez complet et exhaustif>
                Decision: <yes ou no>

                Ne produisez PAS de JSON, de commentaires supplémentaires ou d'autre texte.

                Exemples :

                Exemple positif (pertinent) :
                Tableau:
                | Processeur | Cœurs | Mémoire |
                |------------|-------|---------|
                | Power10    | 16    | 8 TB    |

                Sortie:
                Summary: Le tableau présente les spécifications techniques du processeur Power10. La configuration du processeur comprend 16 cœurs pour des capacités de traitement parallèle. La capacité mémoire prend en charge jusqu'à 8 TB (téraoctets) de RAM, offrant des ressources mémoire importantes pour les charges de travail d'entreprise et les applications intensives en données.
                Decision: yes

                Exemple négatif (non pertinent) :
                Tableau:
                | Préparé par : | John Smith |
                |---------------|------------|

                Sortie:
                Summary: Métadonnées du document indiquant qu'il a été préparé par John Smith.
                Decision: no

                Analysez maintenant le tableau suivant :

                Tableau:
                {content}""",
            description="Prompt de résumé des tableaux (Français)",
        )

    # Language-specific configurations
    english: EnglishConfig = Field(default_factory=EnglishConfig)
    german: GermanConfig = Field(default_factory=GermanConfig)
    italian: ItalianConfig = Field(default_factory=ItalianConfig)
    french: FrenchConfig = Field(default_factory=FrenchConfig)


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
