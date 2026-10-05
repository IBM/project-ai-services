import { Grid, Column, Heading } from '@carbon/react';
import { useParams } from 'react-router';

const ReviewPage = () => {
  const { jobId } = useParams<{ jobId: string }>();

  return (
    <Grid className="cds--css-grid--full-width" style={{ padding: '2rem' }}>
      <Column lg={16} md={8} sm={4}>
        <Heading>Review Invoice: {jobId}</Heading>
        <p style={{ marginTop: '1rem', color: 'var(--cds-text-secondary)' }}>
          Review extracted header fields, line items, and approve/reject staging.
        </p>
      </Column>
    </Grid>
  );
};

export default ReviewPage;
