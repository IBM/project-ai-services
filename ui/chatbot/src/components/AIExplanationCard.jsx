function AIExplanationCard() {
  return (
    <div>
      <h2
        style={{ fontSize: '1.5rem', fontWeight: 400, marginBottom: '0.5rem' }}
      >
        Enterprise Q&A assistance
      </h2>

      <p
        style={{
          fontSize: '0.90rem',
          color: '#525252',
          margin: '0 0.5rem 1.2rem 0',
        }}
      >
        AI interprets your question, retrieves relevant enterprise knowledge,
        and generates a grounded response.
      </p>

      <hr style={{ margin: '1rem 0' }} />

      <div style={{ marginBottom: '1rem' }}>
        <p style={{ marginBottom: '0.3rem' }}>How it works</p>
        <ol style={{ color: '#525252' }}>
          <li>
            1. <strong style={{ color: '#000000ca' }}>Retrieve.</strong> Finds
            the most relevant data using semantic search.
          </li>
          <li>
            2. <strong style={{ color: '#000000ca' }}>Augment.</strong> Combines
            retrieved context with enterprise knowledge.
          </li>
          <li>
            3. <strong style={{ color: '#000000ca' }}>Generate.</strong>{' '}
            Produces an accurate, explainable answer using a large language
            model.
          </li>
        </ol>
      </div>
    </div>
  );
}

export { AIExplanationCard };
