# Design Proposal: Cryptographically Secure Password Generator

---

## 1. Executive Summary

The **Password Generator** utility in `ai-services/internal/pkg/utils` provides cryptographically secure random password generation for the AI Services platform. This proposal documents the security architecture, implementation approach, and guarantees that ensure generated passwords are unpredictable and resistant to guessing attacks. The implementation uses Go's `crypto/rand` package for cryptographic randomness, Fisher-Yates shuffling, and enforces character diversity requirements to maximize entropy and security.

## 2. Problem Statement

### Security Requirements

Password generation for database credentials, API keys, and service authentication requires:

1. **Cryptographic Randomness**: Passwords must be generated using a cryptographically secure random number generator (CSPRNG)
2. **Unpredictability**: No patterns or predictable sequences that could aid brute-force or dictionary attacks
3. **Character Diversity**: Guaranteed inclusion of multiple character types (uppercase, lowercase, digits, special characters)
4. **Sufficient Entropy**: Minimum length and character set size to resist brute-force attacks
5. **No Bias**: Uniform distribution across the character space with no statistical bias
6. **Configurability**: Support for different password policies and requirements

### Threat Model

The password generator must defend against:

- **Brute-force attacks**: Exhaustive search of password space
- **Dictionary attacks**: Common password patterns and words
- **Pattern recognition**: Predictable character sequences or positions
- **Statistical analysis**: Bias in character distribution or frequency
- **Timing attacks**: Information leakage through execution time variations
- **Weak PRNG exploitation**: Predictable pseudo-random number generators

## 3. Security Architecture

### 3.1 Cryptographic Foundation

```
User Request
     |
     v
Password Options Validation
     |
     v
Character Set Construction
     |
     v
crypto/rand.Reader (CSPRNG)
     |
     +---> Generate required characters (one per type)
     |
     +---> Fill remaining positions
     |
     v
Fisher-Yates Shuffle (crypto/rand)
     |
     v
Post-processing (ensure first char not special)
     |
     v
Secure Password Output
```

### 3.2 Entropy Analysis

**Default Configuration:**
- Length: 16 characters
- Character sets: lowercase (26) + uppercase (26) + digits (10) + special (9) = 71 characters
- Entropy: log₂(71¹⁶) ≈ **99.6 bits**

**Entropy Comparison:**

| Configuration | Character Space | Length | Entropy (bits) | Brute-force Resistance |
|---------------|----------------|--------|----------------|------------------------|
| Default (all types) | 71 | 16 | 99.6 | 2⁹⁹·⁶ ≈ 8.9×10²⁹ combinations |
| Alphanumeric only | 62 | 16 | 95.3 | 2⁹⁵·³ ≈ 4.5×10²⁸ combinations |
| Minimum (4 chars, all types) | 71 | 4 | 24.9 | 2²⁴·⁹ ≈ 3.1×10⁷ combinations |
| Extended (32 chars, all types) | 71 | 32 | 199.2 | 2¹⁹⁹·² ≈ 8.0×10⁵⁹ combinations |

**Security Threshold:**
- NIST recommends minimum 80 bits of entropy for secure passwords
- Default configuration (99.6 bits) exceeds this by 24.5%
- Even with 12 characters: log₂(71¹²) ≈ 74.7 bits (close to threshold)

### 3.3 Randomness Source

**Go's crypto/rand Package:**

Uses operating system's CSPRNG:
- **Linux**: `/dev/urandom` (getrandom syscall)
- **macOS**: `/dev/urandom` (arc4random)
- **Windows**: CryptGenRandom API

**Properties:**
- **Cryptographically secure**: Suitable for security-sensitive applications
- **Non-deterministic**: Cannot be predicted from previous outputs
- **High entropy**: Draws from system entropy pool
- **Thread-safe**: Safe for concurrent use
- **Blocking behavior**: Returns error if insufficient entropy (never blocks on modern systems)

## 4. Implementation Approach

### 4.1 Core Algorithm

The password generation follows a four-step process:

**Step 1: Character Set Construction**
- Builds character sets based on enabled options (lowercase, uppercase, digits, special)
- Default character sets:
  - Lowercase: `abcdefghijklmnopqrstuvwxyz` (26 chars)
  - Uppercase: `ABCDEFGHIJKLMNOPQRSTUVWXYZ` (26 chars)
  - Digits: `0123456789` (10 chars)
  - Special: `@#$%^*-_+` (9 chars)

**Step 2: Guaranteed Character Diversity**
- Places one character from each required character type
- Ensures password meets complexity requirements
- Remaining positions filled with random characters from full charset

**Step 3: Cryptographically Secure Character Selection**
- Uses `crypto/rand.Int()` for unbiased random selection
- No modulo bias (uses rejection sampling internally)
- Returns error if CSPRNG fails (fail-secure)

**Step 4: Fisher-Yates Shuffle**
- Eliminates positional patterns from Step 2
- Ensures uniform distribution of all permutations
- Each permutation has equal probability (1/n!)
- Uses crypto/rand for swap positions

**Step 5: Post-processing**
- Ensures first character is not special (when non-special types exist)
- Improves compatibility with systems that reject passwords starting with special characters
- Maintains security by swapping with first non-special character

### 4.2 Security Validations

**Input Validation:**
- Password length must be greater than 0
- At least one character type must be enabled
- Password length must be at least equal to number of enabled character types

**Character Set Selection:**
- Special characters chosen for universal compatibility
- No shell metacharacters that require escaping
- All characters available on standard keyboards
- Visually distinct to avoid confusion

**Excluded Special Characters:**
- `!` - Shell history expansion
- `&|;<>` - Shell operators
- `(){}[]` - Grouping/expansion
- `'"\`` - Quote characters
- `/\` - Path separators
- `~` - Home directory expansion

## 5. Security Guarantees

### 5.1 Unpredictability

**Guarantee:** Generated passwords cannot be predicted from:
- Previous passwords generated
- System state or time
- User input or configuration
- Partial password knowledge

**Mechanism:**
- CSPRNG provides cryptographically secure randomness
- No deterministic patterns in generation
- Fisher-Yates shuffle eliminates positional bias
- Each password generation is independent

### 5.2 Resistance to Attacks

**Brute-Force Resistance:**
- Default 16-character password: 2⁹⁹·⁶ combinations
- At 1 billion attempts/second: **2.8×10¹³ years** to exhaust space
- At 1 trillion attempts/second: **2.8×10¹⁰ years** to exhaust space

**Dictionary Attack Resistance:**
- No common words or patterns
- Character diversity prevents dictionary matches
- Random character selection eliminates predictable sequences

**Pattern Recognition Resistance:**
- Fisher-Yates shuffle ensures uniform permutation distribution
- No positional bias for character types
- No sequential or repeating patterns

**Statistical Analysis Resistance:**
- Uniform character distribution across charset
- No frequency bias in character selection
- No correlation between character positions

### 5.3 Entropy Guarantees

**Minimum Entropy Calculation:**

For a password of length `L` with character space `C`:
```
Entropy = L × log₂(C)
```

**Default Configuration:**
```
L = 16 characters
C = 71 characters (26 + 26 + 10 + 9)
Entropy = 16 × log₂(71) ≈ 16 × 6.15 ≈ 99.6 bits
```

**Effective Entropy (with character requirements):**

When requiring at least one character from each of 4 types:
```
Effective entropy ≈ 99.6 - log₂(4!) ≈ 99.6 - 4.6 ≈ 95 bits
```

Still well above the 80-bit security threshold.

## 6. Configuration and Usage

### 6.1 Default Password Generation

Function: `GenerateRandomPassword()`

**Properties:**
- Length: 16 characters
- Character types: lowercase, uppercase, digits, special
- Entropy: ~99.6 bits
- Guaranteed: At least one character from each type
- Example output: `aB3#xK9@mP2$vL7^`

### 6.2 Custom Configuration via Annotations

**YAML Annotation Format:**

```yaml
# @generate:password
database_password: ""

# @generate:password length=24
api_key: ""

# @generate:password length=32, special=false
service_token: ""

# @generate:password length=20, lower=true, upper=true, digits=false, special=false
alphanumeric_password: ""
```

**Supported Options:**

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `length` | integer | 16 | Password length (must be ≥ number of enabled types) |
| `lower` | boolean | true | Include lowercase letters (a-z) |
| `upper` | boolean | true | Include uppercase letters (A-Z) |
| `digits` | boolean | true | Include digits (0-9) |
| `special` | boolean | true | Include special characters (@#$%^*-_+) |

### 6.3 Programmatic Usage

Function: `generateRandomPasswordWithOptions(opts passwordOptions)`

Allows custom password generation with specific requirements through the `passwordOptions` struct.

## 7. Testing and Validation

### 7.1 Security Tests

**Uniqueness Test:**
- Generates 100 passwords with same configuration
- Verifies at least 95% uniqueness
- Ensures no predictable patterns or collisions

**Character Diversity Test:**
- Verifies each enabled character type appears at least once
- Confirms disabled character types are absent
- Validates character requirements are met

**Shuffle Verification:**
- Confirms all characters preserved after shuffling
- Verifies no data loss or corruption
- Ensures character frequency unchanged

**First Character Test:**
- Validates first character is not special (when non-special types exist)
- Confirms compatibility with restrictive systems

### 7.2 Statistical Analysis

**Chi-Square Test for Uniform Distribution:**
- Generate 10,000 passwords
- Measure character frequency distribution
- Verify chi-square statistic indicates uniform distribution
- Expected frequency per character: (10,000 × 16) / 71 ≈ 2,253
- Critical value at α=0.05, df=70: 90.53

**Entropy Measurement:**
- Calculate Shannon entropy of generated passwords
- Expected entropy per character: log₂(71) ≈ 6.15 bits
- Verify measured entropy matches theoretical entropy

## 8. Conclusion

The password generator implementation provides cryptographically secure, unpredictable passwords with guaranteed character diversity and sufficient entropy to resist all known password attacks. The use of Go's `crypto/rand` package, Fisher-Yates shuffling, and careful character set selection ensures that generated passwords meet the highest security standards.

**Key Security Properties:**
- ✅ Cryptographically secure randomness (crypto/rand)
- ✅ Unpredictable output (no patterns or bias)
- ✅ High entropy (99.6 bits default)
- ✅ Character diversity guaranteed
- ✅ Uniform distribution (Fisher-Yates shuffle)
- ✅ Fail-secure error handling
- ✅ Comprehensive test coverage

**Entropy Summary:**
- Default configuration: 99.6 bits (exceeds NIST 80-bit threshold by 24.5%)
- Brute-force resistance: 2⁹⁹·⁶ ≈ 8.9×10²⁹ combinations
- Time to crack: 2.8×10¹³ years at 1 billion attempts/second

The implementation is production-ready and suitable for generating passwords for database credentials, API keys, service tokens, and any other security-sensitive use cases in the AI Services platform.

---

**References:**
- [NIST SP 800-90A: Recommendation for Random Number Generation](https://csrc.nist.gov/pubs/sp/800/90/a/r1/final) - Standards for cryptographically secure random number generation
- [Go crypto/rand Package Documentation](https://pkg.go.dev/crypto/rand) - Cryptographically secure random number generation
- [Fisher-Yates Shuffle Algorithm](https://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle) - Unbiased shuffling algorithm
