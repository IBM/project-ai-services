# Language Support: Experimentation, Analysis, and Final Implementation

## 1. Objective

Enable **cross-lingual retrieval support** for:

- **Currently Supported Languages**: English (EN), German (DE)  
- **Planned Extension (Q1)**: French (FR), Italian (IT)

The goal of this initiative is to:

1. Support semantic retrieval across multiple languages.
2. Detect the user query language reliably.
3. Generate responses in the same language as the user query.

---

## 2. Plan

To support cross-lingual retrieval, we decided to leverage **multilingual embedding models**, relying on their ability to capture semantic similarity across languages.

We evaluated the following embedding models:

- `granite-278m-multilingual` embedding model  
- `multilingual-e5` (ME5) embedding model  

> **Note:** All experiments were conducted after migrating to **OpenSearch** as the vector database (replacing Milvus).

---
## 3. Intended Solution

We rely on multilingual embedding models to perform cross-lingual information retrieval. 

Once the relevant documents (context) are retrieved:

1. The user query language is detected using our internal `lang_detection` utility  
   *(detailed in a later section)*.
2. Based on the detected language (`en` or `de`), the corresponding language-specific prompt is selected.
3. The selected prompt, along with the retrieved context, is passed to the LLM for response generation.
---


## 4. Key Problems Addressed

We addressed two primary questions:

1. **Which embedding model should be used for the retrieval layer?**
   - Granite-278m-multilingual  
   - Multilingual-E5  

2. **What is the most appropriate language detection strategy?**
---

## 5. Experimentation 1: Embedding Model Selection (Retrieval Layer)

#### Points to be noted before analyzing below table 

-	There is no test conducted on German corpus at this point, as the current existing logic doesn’t support creating a golden dataset that spawns through German corpus. But it is in progress.
-	After the integration of opensearch, we have seen that the generated answers are being more elaborative ( also contain extra facts), thus moving far from the golden dataset answers 
-	We created the german dataset on English corpus by simply translating the English dataset using a llm flow


---

### Evaluation Results (AI Builder Studio)

| Model        | Dataset | No Context = No Answer | Not Judged by LLM | Result  | Comments                                                    |
|--------------|----------|------------------------|------------------|---------|-------------------------------------------------------------|
| ME5          | English  | 1                      | 7                | 96–98%  | Most “not judged” responses were correct upon manual review |
| ME5          | German   | 6                      | 12               | 80%     | 4 of 12 not-judged responses were slightly inaccurate. Hence 80%      |
| Granite-278m | English  | 1                      | 10               | 96–98%  | Most “not judged” responses were correct upon manual review |
| Granite-278m | German   | 3                      | 13               | 86%     | 4 of 13 not-judged responses were slightly inaccurate. Hence 80%                   |


### Conclusion (Embedding Model)

Although both models performed similarly on the English dataset, **Granite-278m-multilingual embedding** demonstrated better consistency on the German dataset during retrieval evaluation.

#### ✅ Decision

**Granite-278m-multilingual embedding model** has been selected as the embedding model for cross-lingual retrieval.

---

## 6. Experimentation 2: Language Detection Strategy

Once the relevant context is retrieved, the next step is to ensure that the LLM responds in the same language as the user query.

We evaluated four approaches:

---

### Approach 1: Rule-Based Detection (`langdetect`) library

GitHub: https://github.com/Mimino666/langdetect

```python
from langdetect import detect

def detect_language(text):
    try:
        return detect(text)   # returns 'en', 'de', etc.
    except:
        return "unknown"

prompt = "Wie ist das Wetter heute?"
lang = detect_language(prompt)

if lang == "de":
    system_prompt = "Du bist ein deutscher Assistent ..."
else:
    system_prompt = "You are an English assistant ..."

```


#### Pros

- Easy integration  
- Lightweight  
- Fast execution  

#### Cons

- Not actively maintained  
- Port of an older Google solution  
- Maintained by a single contributor  

#### ❌ Verdict

Not considered reliable enough for production usage.

---

### Approach 2: Dedicated Language ID Models

#### Examples

- fastText (`lid.176.bin`)
- CLD3
- langid.py

**Reference:**  
https://huggingface.co/facebook/fasttext-language-identification

```python
import fasttext
from huggingface_hub import hf_hub_download

model_path = hf_hub_download(repo_id="facebook/fasttext-language-identification", filename="model.bin")
model = fasttext.load_model(model_path)
model.predict("Hello, world!")

(('__label__eng_Latn',), array([0.81148803]))

model.predict("Hello, world!", k=5)

(('__label__eng_Latn', '__label__vie_Latn', '__label__nld_Latn', '__label__pol_Latn', '__label__deu_Latn'), 
 array([0.61224753, 0.21323682, 0.09696738, 0.01359863, 0.01319415]))
```

#### Pros

- Industry standard
- High accuracy
- Designed specifically for language detection
- Fast inference

#### Cons

- It’s an additional overhead to download a new model just for this purpose. ( model size is around 1.2 Gb)
- Additional licensing and dependency overhead

#### ❌ Verdict

Reliable and technically robust, but operationally heavy and unnecessary for the current scope.


---

### Approach 3: Language Detection Using Embeddings

Since embeddings are already available, we tested language detection using cosine similarity.

#### Flow

1. Create reference embeddings for each supported language.
2. Embed the user query.
3. Compute cosine similarity.
4. Select the language with the highest similarity score.

#### Additional Optimizations

- Increased centroid vectors.
- Applied L2 normalization before computing cosine similarity.

```python
def cosine(a, b):
    return np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b))

def detect_language(query, emb_model, emb_endpoint, max_tokens):
    embedder = get_embedder(emb_model, emb_endpoint, max_tokens)
    user_emb = embedder.embed_query(query)
    ref_texts = {
        "en": "This is an English sentence.",
        "de": "Dies ist ein deutscher Satz."
    }
    ref_embeddings = {
        lang: embedder.embed_query(text)
        for lang, text in ref_texts.items()
    }
    
    scores = {
        lang: cosine(user_emb, ref_emb)
        for lang, ref_emb in ref_embeddings.items()
    }
    logger.debug(f"Language detection scores: {scores}")
    return max(scores, key=scores.get)
```

A sample code for this has been written in my early PR - https://github.com/IBM/project-ai-services/pull/321

---

#### Results

| Model            | Language Detection Accuracy | Comments                                       |
|------------------|----------------------------|-----------------------------------------------|
| Multilingual-E5  | 100%                       | Correct detection for all EN and DE queries   |
| Granite-278m     | 20–30%                     | Frequently defaulted to English               |

---

#### ❌ Verdict

Although **Multilingual-E5** performed perfectly for language detection, **Granite-278m** (selected for retrieval) did not perform well in this task.

Maintaining two separate embedding models (one for retrieval and another for language detection) was considered unnecessary architectural complexity.

---


### Approach 4: LLM-Based Language Detection (Final Approach)


Since some of the above approaches are not practical, and some don’t work as expected -  We have decided to use a single multilingual prompt, allowing the LLM to detect the language of the query, by giving it a chain of reasoning to first silently detect the lang, and then respond per the detected lang in the same LLM request


---

#### Multilingual Prompt

```text
You are given:
1. A short context text containing factual information.
2. A user's question seeking clarification or advice.

Your task:
• First, silently determine whether the question is written in English or German.
• Then, answer strictly in the same language as the question.
  (English → respond in English; German → auf Deutsch antworten)
• Return a concise, to-the-point answer grounded strictly in the provided context.
• The answer must be accurate, easy to follow, and include clear reasoning.
• If the context does not provide enough information, answer using general knowledge.
```


> **Note** - The prompt was refined through multiple iterations and can be further hardened to suppress meta-information if required.

### Advantages of LLM-Based Detection

- No additional models required  
- No new dependencies  
- Scalable to additional languages  
- Minimal prompt overhead  
- Leverages Granite-Instruct’s multilingual capability  

---

### Limitations

- Rare edge-case misclassification  
- Occasional meta-information leakage 

---

Now using this solution, and granite-278m-multilingual-embedding model as the embedder, we have the following results:

## Results with LLM-Based Detection

### Language Detection Accuracy 
(if the LLM is responding back in the same language)

| Language | Result       | Comments            |
|----------|--------------|--------------------|
| German   | 98% (49/50)  |Only for 1 question, the LLM guessed wrong as EN, and responded in English. |
| English  | 100% (50/50) | All the queries were detected in English, and responded in English.     |

---



### Retrieval + Answer Evaluation
#### Accuracy and Evaluations of the inferred answers on English corpus with this solution – Final **

| Dataset | Not Answered | Not Judged by LLM | Result             |
|----------|--------------|------------------|-------------------|
| German   | 3            | 10               |  |
| English  | 1            | 9                | 96–98%             |


## 7. Conclusion and Final Architecture

### Embedding Model

✅ **Granite-278m-multilingual**  
Used for cross-lingual semantic retrieval.

---

### Language Handling

✅ **Single multilingual LLM prompt**  
Used for implicit language detection and response generation.

---

### Final Architecture

```text
User Query
    ↓
Granite-278m-multilingual Embedding
    ↓
OpenSearch (Vector Retrieval)
    ↓
Retrieved Context
    ↓
Granite-Instruct LLM
    • Detect language internally
    • Respond in same language
    ↓
Final Response
```
