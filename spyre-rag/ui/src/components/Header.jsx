import {
  Header,
  HeaderName,
} from "@carbon/react";

const HeaderNav = () => {

  return (
    <Header aria-label="">
    <HeaderName to="/" prefix="">
        DocuAssistant &#8482;
    </HeaderName>
    </Header>
  );
};

export default HeaderNav;
