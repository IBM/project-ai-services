import { Grid, Column, Heading } from '@carbon/react';

const JobsPage = () => {
  return (
    <Grid className="cds--css-grid--full-width" style={{ padding: '2rem' }}>
      <Column lg={16} md={8} sm={4}>
        <Heading>Invoice Processing Jobs</Heading>
        <p style={{ marginTop: '1rem', color: 'var(--cds-text-secondary)' }}>
          Monitor status, pipeline progress, and processing history.
        </p>
      </Column>
    </Grid>
  );
};

export default JobsPage;
