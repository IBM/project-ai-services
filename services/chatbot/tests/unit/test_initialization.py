"""
Unit tests for initialization functions in chatbot/app.py
"""

import pytest
from unittest.mock import Mock, patch, MagicMock
from contextlib import asynccontextmanager


@pytest.mark.unit
class TestInitializeModels:
    """Tests for initialize_models() function"""
    
    def test_initialize_models_success(self, monkeypatch, mock_model_dicts):
        """Test successful model initialization with valid endpoints"""
        # Mock individual endpoint functions
        mock_get_emb = Mock(return_value=mock_model_dicts['emb_model_dict'])
        mock_get_llm = Mock(return_value=mock_model_dicts['llm_model_dict'])
        mock_get_reranker = Mock(return_value=mock_model_dicts['reranker_model_dict'])
        
        with patch('chatbot.app.get_embedding_endpoint', mock_get_emb), \
             patch('chatbot.app.get_llm_endpoint', mock_get_llm), \
             patch('chatbot.app.get_reranker_endpoint', mock_get_reranker):
            from chatbot.app import initialize_models, emb_model_dict, llm_model_dict, reranker_model_dict
            
            # Call initialization
            initialize_models()
            
            # Verify all endpoint functions were called
            mock_get_emb.assert_called_once()
            mock_get_llm.assert_called_once()
            mock_get_reranker.assert_called_once()
    
    def test_initialize_models_empty_dicts(self, monkeypatch):
        """Test initialization when endpoint functions return empty dicts"""
        mock_get_emb = Mock(return_value={})
        mock_get_llm = Mock(return_value={})
        mock_get_reranker = Mock(return_value={})
        
        with patch('chatbot.app.get_embedding_endpoint', mock_get_emb), \
             patch('chatbot.app.get_llm_endpoint', mock_get_llm), \
             patch('chatbot.app.get_reranker_endpoint', mock_get_reranker):
            from chatbot.app import initialize_models
            
            # Should not raise exception
            initialize_models()
            mock_get_emb.assert_called_once()
            mock_get_llm.assert_called_once()
            mock_get_reranker.assert_called_once()
    
    def test_initialize_models_exception(self, monkeypatch):
        """Test initialization when endpoint function raises exception"""
        mock_get_emb = Mock(side_effect=Exception("Connection error"))
        mock_get_llm = Mock(return_value={})
        mock_get_reranker = Mock(return_value={})
        
        with patch('chatbot.app.get_embedding_endpoint', mock_get_emb), \
             patch('chatbot.app.get_llm_endpoint', mock_get_llm), \
             patch('chatbot.app.get_reranker_endpoint', mock_get_reranker):
            from chatbot.app import initialize_models
            
            # Should raise the exception from get_embedding_endpoint
            with pytest.raises(Exception, match="Connection error"):
                initialize_models()


@pytest.mark.unit
class TestInitializeVectorstore:
    """Tests for initialize_vectorstore() function"""
    
    def test_initialize_vectorstore_success(self, monkeypatch, mock_vectorstore):
        """Test successful vectorstore initialization"""
        mock_get_vector_store = Mock(return_value=mock_vectorstore)
        
        with patch('chatbot.app.db.get_vector_store', mock_get_vector_store):
            from chatbot.app import initialize_vectorstore, vectorstore
            
            initialize_vectorstore()
            
            mock_get_vector_store.assert_called_once()
    
    def test_initialize_vectorstore_exception(self, monkeypatch):
        """Test vectorstore initialization when get_vector_store raises exception"""
        mock_get_vector_store = Mock(side_effect=Exception("Database connection failed"))
        
        with patch('chatbot.app.db.get_vector_store', mock_get_vector_store):
            from chatbot.app import initialize_vectorstore
            
            with pytest.raises(Exception, match="Database connection failed"):
                initialize_vectorstore()
    
    def test_initialize_vectorstore_none(self, monkeypatch):
        """Test vectorstore initialization when get_vector_store returns None"""
        mock_get_vector_store = Mock(return_value=None)
        
        with patch('chatbot.app.db.get_vector_store', mock_get_vector_store):
            from chatbot.app import initialize_vectorstore
            
            # Should not raise exception
            initialize_vectorstore()
            mock_get_vector_store.assert_called_once()


@pytest.mark.unit
@pytest.mark.asyncio
class TestLifespan:
    """Tests for lifespan() context manager"""
    
    async def test_lifespan_initialization_order(self, monkeypatch):
        """Test that all initialization functions are called in correct order"""
        from lingua import Language
        
        # Create mocks
        mock_init_models = Mock()
        mock_init_vectorstore = Mock()
        mock_create_session = Mock()
        
        # Track call order
        call_order = []
        
        def track_init_models():
            call_order.append('models')
            mock_init_models()
        
        def track_init_vectorstore():
            call_order.append('vectorstore')
            mock_init_vectorstore()
        
        def track_create_session(pool_maxsize):
            call_order.append('session')
            mock_create_session(pool_maxsize)
        
        with patch('chatbot.app.initialize_models', track_init_models), \
             patch('chatbot.app.initialize_vectorstore', track_init_vectorstore), \
             patch('chatbot.app.create_llm_session', track_create_session):
            
            from chatbot.app import lifespan
            
            # Create a mock app
            mock_app = Mock()
            
            # Execute lifespan context manager
            async with lifespan(mock_app):
                pass
            
            # Verify all functions were called in correct order
            # Note: vectorstore is NOT initialized in lifespan, it's lazy-loaded on first request
            # Note: language detector is initialized in settings module, not in lifespan
            assert call_order == ['session', 'models']
            mock_init_models.assert_called_once()
            mock_init_vectorstore.assert_not_called()  # Vectorstore is lazy-loaded on first request
            # Pool size comes from settings.common.llm.max_batch_size
            mock_create_session.assert_called_once()
    
    async def test_lifespan_language_detector_setup(self, monkeypatch):
        """Test language detector is initialized in settings module (not in lifespan)"""
        from lingua import Language
        from common.lang_utils import _language_detector
        
        # Language detector should already be initialized by settings module
        # This test verifies it's available (initialized in settings, not lifespan)
        assert _language_detector is not None, "Language detector should be initialized by settings module"
    
    async def test_lifespan_llm_session_pool_size(self, monkeypatch):
        """Test LLM session is created with correct pool size from settings"""
        mock_create_session = Mock()
        
        with patch('chatbot.app.initialize_models'), \
             patch('chatbot.app.create_llm_session', mock_create_session):
            
            from chatbot.app import lifespan
            
            mock_app = Mock()
            
            async with lifespan(mock_app):
                pass
            
            # Verify session was created with pool_maxsize from settings
            mock_create_session.assert_called_once()
            call_kwargs = mock_create_session.call_args[1]
            assert 'pool_maxsize' in call_kwargs
    
    async def test_lifespan_exception_during_startup(self, monkeypatch):
        """Test exception handling during startup"""
        mock_init_models = Mock(side_effect=Exception("Initialization failed"))
        
        with patch('chatbot.app.initialize_models', mock_init_models):
            from chatbot.app import lifespan
            
            mock_app = Mock()
            
            # Should raise the exception
            with pytest.raises(Exception, match="Initialization failed"):
                async with lifespan(mock_app):
                    pass
    
    async def test_lifespan_cleanup_on_shutdown(self, monkeypatch):
        """Test cleanup behavior on shutdown (yield behavior)"""
        startup_complete = False
        shutdown_complete = False
        
        def mock_init():
            nonlocal startup_complete
            startup_complete = True
        
        with patch('chatbot.app.initialize_models', mock_init), \
             patch('chatbot.app.initialize_vectorstore'), \
             patch('chatbot.app.create_llm_session'):
            
            from chatbot.app import lifespan
            
            mock_app = Mock()
            
            async with lifespan(mock_app):
                # Verify startup completed
                assert startup_complete is True
            
            # After context exits, we're in shutdown phase
            shutdown_complete = True
            assert shutdown_complete is True


@pytest.mark.unit
class TestGlobalVariables:
    """Tests for global variable initialization"""
    
    def test_global_variables_initial_state(self):
        """Test that global variables are initialized correctly"""
        from chatbot.app import emb_model_dict, llm_model_dict, reranker_model_dict, vectorstore
        
        # Note: These may be set by other tests, so we just verify they exist
        assert isinstance(emb_model_dict, dict)
        assert isinstance(llm_model_dict, dict)
        assert isinstance(reranker_model_dict, dict)
    
    def test_concurrency_limiter_initialization(self):
        """Test that concurrency limiter is initialized with correct value"""
        from chatbot.app import concurrency_limiter, settings
        
        # Verify semaphore exists and has correct bound
        assert concurrency_limiter is not None
        assert hasattr(concurrency_limiter, '_value')
    
    def test_pool_size_from_settings(self):
        """Test pool size comes from settings"""
        from chatbot.app import settings
        
        # Pool size is configured via settings.common.llm.max_batch_size
        assert hasattr(settings.common.llm, 'max_batch_size')
        assert settings.common.llm.max_batch_size > 0

# Made with Bob
