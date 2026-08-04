export interface UseCase {
  id: string;
  title: string;
  description: string;
  creator: string;
  domain: string;
  architectures: string[];
  assets: string[];
  tag: string[];
  demo?: string;
  demoTitle?: string;
  demoDescription?: string;
  demoDuration?: string;
  featuredArticle?: {
    company: string;
    title: string;
    imagePath: string;
    articleUrl: string;
    description?: string;
  };
  clientStories?: Array<{
    company: string;
    description?: string;
    url?: string;
  }>;
  partnerStories?: Array<{
    company: string;
    description?: string;
    url?: string;
    testimonial?: string;
  }>;
  testimonial?: {
    company: string;
    quote: string;
  };
}
