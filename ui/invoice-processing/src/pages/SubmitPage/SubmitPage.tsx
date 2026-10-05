import { Grid, Column, Heading } from '@carbon/react';

const SubmitPage = () => {
  return (
    <Grid className="cds--css-grid--full-width" style={{ padding: '2rem' }}>
      <Column lg={16} md={8} sm={4}>
        <Heading>Submit Invoice</Heading>
        <p style={{ marginTop: '1rem', color: 'var(--cds-text-secondary)' }}>
          Upload invoice PDF or scanned image to start processing.
        </p>
      </Column>
    </Grid>
  );
};

export default SubmitPage;
