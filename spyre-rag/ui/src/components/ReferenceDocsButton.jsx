import { useState } from 'react';
import { Button, Tile } from '@carbon/react';

function ReferenceDocsButton({ data }) {
  const [open, setOpen] = useState(false);

  return (
    <div style={{ marginTop: '1rem' }}>
      <Button kind="tertiary" size="md" onClick={() => setOpen(!open)}>
        {open
          ? 'Hide reference documents'
          : data.button_label || 'Get reference documents'}
      </Button>

      {open && (
        <div
          style={{
            marginTop: '0.75rem',
            display: 'grid',
            gap: '1rem',
            maxHeight: '40vh',
            overflowY: 'auto',
          }}
        >
          {data?.docs?.map((doc, i) => (
            <Tile key={i}>
              <p className="cds--label-01">
                <strong>{doc.filename}</strong>
              </p>
              <p className="cds--body-01">{doc.page_content}</p>
            </Tile>
          ))}
        </div>
      )}
    </div>
  );
}

export { ReferenceDocsButton };
