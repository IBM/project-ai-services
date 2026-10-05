import { useState } from 'react';
import { useNavigate } from 'react-router';
import {
  Header,
  HeaderName,
  HeaderGlobalBar,
  HeaderGlobalAction,
  HeaderMenuButton,
  Theme,
  Modal,
} from '@carbon/react';
import { Help } from '@carbon/icons-react';
import { useTheme } from '@contexts/useTheme';
import ThemeSwitcher from '@components/ThemeSwitcher/ThemeSwitcher';
import styles from './AppHeader.module.scss';

interface AppHeaderProps {
  isSideNavOpen: boolean;
  setIsSideNavOpen: React.Dispatch<React.SetStateAction<boolean>>;
}

const AppHeader = ({ isSideNavOpen, setIsSideNavOpen }: AppHeaderProps) => {
  const { effectiveTheme } = useTheme();
  const navigate = useNavigate();
  const [isHelpModalOpen, setIsHelpModalOpen] = useState(false);

  const handleHelpClick = () => {
    setIsHelpModalOpen(true);
  };

  const handleCloseModal = () => {
    setIsHelpModalOpen(false);
  };

  const handleLogoClick = () => {
    navigate('/');
  };

  return (
    <>
      <Header aria-label="IBM AI Services" className={styles.header}>
        <HeaderMenuButton
          aria-label="Open menu"
          onClick={(e) => {
            e.stopPropagation();
            setIsSideNavOpen((prev) => !prev);
          }}
          isActive={isSideNavOpen}
          isCollapsible
          className={styles.menuBtn}
        />

        <HeaderName prefix="IBM" href="#" onClick={handleLogoClick} className={styles.headerName}>
          Invoice Processing
        </HeaderName>

        <HeaderGlobalBar>
          <ThemeSwitcher />
          <HeaderGlobalAction
            aria-label="Help"
            className={styles.iconWidth}
            onClick={handleHelpClick}
          >
            <Help size={20} />
          </HeaderGlobalAction>
        </HeaderGlobalBar>
      </Header>

      <Theme theme={effectiveTheme}>
        <Modal
          open={isHelpModalOpen}
          onRequestClose={handleCloseModal}
          modalHeading="Help & Documentation"
          primaryButtonText="Close"
          onRequestSubmit={handleCloseModal}
          size="md"
          className={styles.helpModal}
        >
          <div className={styles.helpContent}>
            <h4>Welcome to Invoice Processing</h4>
            <p>
              This application automates end-to-end invoice ingestion, extraction, and ERP staging.
            </p>

            <h5>Key Features:</h5>
            <ul>
              <li><strong>Submit:</strong> Upload invoice PDFs or scanned images</li>
              <li><strong>Jobs:</strong> Monitor pipeline execution status and stages</li>
              <li><strong>Review:</strong> Review, edit extracted AP headers and line items, approve or reject</li>
            </ul>

            <h5>Getting Started:</h5>
            <ol>
              <li>Navigate to <strong>Submit</strong> to upload an invoice</li>
              <li>Track progress in the <strong>Jobs</strong> monitor</li>
              <li>Inspect and confirm extracted fields in <strong>Review</strong> before DB load</li>
            </ol>
          </div>
        </Modal>
      </Theme>
    </>
  );
};

export default AppHeader;
