import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
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
            AI Services
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
            <h4>Welcome to Digitize Service</h4>
            <p>
              This application helps you digitize and process documents efficiently.
            </p>
            
            <h5>Key Features:</h5>
            <ul>
              <li><strong>Document Upload:</strong> Upload PDF files for processing</li>
              <li><strong>Document List:</strong> View and manage all your uploaded documents</li>
              <li><strong>Job Monitor:</strong> Track the status of document processing jobs</li>
            </ul>

            <h5>Getting Started:</h5>
            <ol>
              <li>Navigate to the <strong>Jobs</strong> page to upload your documents</li>
              <li>Monitor processing status in the <strong>Jobs</strong> page</li>
              <li>View processed documents in the <strong>Documents</strong> page</li>
            </ol>

            <h5>Need More Help?</h5>
            <p>
              For detailed documentation and support, please visit the{' '}
              <a
                href="https://www.ibm.com/docs/en/aiservices/2025.12.0?topic=services-introduction"
                target="_blank"
                rel="noopener noreferrer"
              >
                IBM AI Services documentation
              </a>{' '}
              or contact your system administrator.
            </p>
          </div>
        </Modal>
      </Theme>
    </>
  );
};

export default AppHeader;

// Made with Bob