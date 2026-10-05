import { Grid, Column, Heading } from '@carbon/react';
import { useParams } from 'react-router';

const JobDetailPage = () => {
  const { jobId } = useParams<{ jobId: string }>();

  return (
    <Grid className="cds--css-grid--full-width" style={{ padding: '2rem' }}>
      <Column lg={16} md={8} sm={4}>
        <Heading>Job Detail: {jobId}</Heading>
        <p style={{ marginTop: '1rem', color: 'var(--cds-text-secondary)' }}>
          Detailed view of invoice processing job stages, latency, and extraction output.
        </p>
      </Column>
    </Grid>
  );
};

export default JobDetailPage;
